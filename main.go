package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"github.com/Azure/azure-sdk-for-go/sdk/azidentity"
	"github.com/Azure/azure-sdk-for-go/sdk/security/keyvault/azkeys"
	"github.com/Azure/azure-sdk-for-go/sdk/security/keyvault/azsecrets"
	"github.com/IBM/sarama"
	_ "github.com/lib/pq"
	"k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/client-go/util/homedir"
	"log"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"path/filepath"
	"strings"

	"scan-queue-processor/constants"
	"scan-queue-processor/dynamic_resource_allocator"
	"scan-queue-processor/models"
	"scan-queue-processor/scm_service"
)

func main() {
	// Handle signals to gracefully shut down the consumer
	signals := make(chan os.Signal, 1)
	signal.Notify(signals, os.Interrupt)

	//config, err := rest.InClusterConfig() // Use this for in-cluster configuration.
	// OR
	kubeconfigPath := filepath.Join(homedir.HomeDir(), ".kube", "config")
	kubeConfig, err := clientcmd.BuildConfigFromFlags("", kubeconfigPath) // Use this for out-of-cluster configuration.

	if err != nil {
		panic(err.Error())
	}

	clientSet, err := kubernetes.NewForConfig(kubeConfig)
	if err != nil {
		panic(err.Error())
	}

	//ticker := time.NewTicker(15 * time.Second)
	//defer ticker.Stop()

	namespace := "trufflehog-scanner"
	//go func() {
	//	for {
	//		select {
	//		case <-ticker.C:
	//			// Call your monitorAndCleanup function here.
	//			monitorAndCleanup(clientSet, namespace)
	//		}
	//	}
	//}()

	cred, err := azidentity.NewDefaultAzureCredential(nil)
	if err != nil {
		panic(err)
	}

	keyVaultURL := os.Getenv("KEY_VAULT_URL")

	secretClient, err := azsecrets.NewClient(keyVaultURL, cred, nil)
	if err != nil {
		panic(err)
	}

	azkeysClient, err := azkeys.NewClient(keyVaultURL, cred, nil)
	if err != nil {
		panic(err)
	}

	githubAPIToken, err := secretClient.GetSecret(context.TODO(), constants.GithubAPITokenKey, "", nil)
	if err != nil {
		panic(err)
	}

	github := scm_service.NewGithub(*http.DefaultClient, azkeysClient, *githubAPIToken.Value)

	dbPassword, err := secretClient.GetSecret(context.TODO(), constants.DBPasswordKey, "", nil)
	if err != nil {
		panic(err)
	}

	// PostgreSQL connection details
	dbInfo := url.URL{
		Scheme:   "postgres",
		User:     url.UserPassword(os.Getenv("DB_USER"), *dbPassword.Value),
		Host:     fmt.Sprintf("%s:%s", os.Getenv("DB_HOST"), os.Getenv("DB_PORT")),
		Path:     os.Getenv("DB_NAME"),
		RawQuery: "sslmode=disable",
	}

	// Connect to the database
	db, err := sql.Open("postgres", dbInfo.String())
	if err != nil {
		log.Fatal(err)
	}
	defer db.Close()

	// Check the connection
	if err := db.Ping(); err != nil {
		log.Fatal(err)
	}

	resourceManager := dynamic_resource_allocator.NewResourceManager(clientSet, github, db, namespace)

	// Define the Kafka broker addresses
	brokers := []string{os.Getenv("KAFKA_HOST")} // Replace with your broker addresses

	// Create a new consumer
	kafkaConfig := sarama.NewConfig()
	consumer, err := sarama.NewConsumer(brokers, kafkaConfig)
	if err != nil {
		log.Fatalf("Error creating consumer: %v", err)
	}

	defer func() {
		if err := consumer.Close(); err != nil {
			log.Fatalf("Error closing consumer: %v", err)
		}
	}()

	// Specify the topic you want to consume from

	// Create a partition consumer
	topic := os.Getenv("TOPIC_NAME")
	partitionConsumer, err := consumer.ConsumePartition(topic, 0, sarama.OffsetNewest)
	if err != nil {
		log.Fatalf("Error creating partition consumer: %v", err)
	}
	defer func() {
		if err := partitionConsumer.Close(); err != nil {
			log.Fatalf("Error closing partition consumer: %v", err)
		}
	}()

	// Consume messages
	for {
		select {
		case msg := <-partitionConsumer.Messages():
			fmt.Printf("Received message: %s\n", string(msg.Value))
			var scanRequest models.QueuedScan
			err := json.Unmarshal(msg.Value, &scanRequest)
			if err != nil {
				continue
			}
			err = resourceManager.ProcessRequest(context.TODO(), scanRequest)
			if err != nil {
				log.Printf("Unable to process request %v", err)
				continue
			}
		case <-signals:
			fmt.Println("Received shutdown signal. Closing consumer...")
			return
		}
	}
}

func monitorAndCleanup(clientset *kubernetes.Clientset, namespace string) {
	// Define the PV name for which you want to get associated PVCs.
	pvName := "trufflehog-pv"

	list, err := clientset.CoreV1().PersistentVolumes().List(context.TODO(), metav1.ListOptions{})
	if err != nil {
		log.Panicln("unable to get pv ", err)
		return
	}

	volumes := []v1.PersistentVolume{}

	for _, pv := range list.Items {
		if strings.HasPrefix(pv.Name, "trufflehog") {
			volumes = append(volumes, pv)
		}
	}

	// Get the PV object.
	pv, err := clientset.CoreV1().PersistentVolumes().Get(context.TODO(), pvName, metav1.GetOptions{})
	if err != nil {
		panic(err.Error())
	}

	// Get the list of PVCs associated with the PV.
	pvcList, err := getAssociatedPVCs(clientset, pv)
	if err != nil {
		panic(err.Error())
	}

	pods, err := clientset.CoreV1().Pods(namespace).List(context.TODO(), metav1.ListOptions{})
	if err != nil {
		panic(err)
	}

	// Create a map to track PVC usage.
	pvcUsage := make(map[string]bool)

	// Mark PVCs as used by Pods.
	for _, pod := range pods.Items {
		for _, volume := range pod.Spec.Volumes {
			if volume.PersistentVolumeClaim != nil {
				pvcName := volume.PersistentVolumeClaim.ClaimName
				pvcUsage[pvcName] = true
			}
		}
	}

	// Find unused PVCs.
	var unusedPVCs []*v1.PersistentVolumeClaim
	for _, pvc := range pvcList {
		pvcName := pvc.Name
		if _, used := pvcUsage[pvcName]; !used {
			unusedPVCs = append(unusedPVCs, pvc)
		}
	}

	for _, unusedPVC := range unusedPVCs {
		err := clientset.CoreV1().PersistentVolumeClaims(namespace).Delete(context.TODO(), unusedPVC.Name, metav1.DeleteOptions{})
		if err != nil {
			panic(err.Error())
		}
	}

}

func getAssociatedPVCs(clientset *kubernetes.Clientset, pv *v1.PersistentVolume) ([]*v1.PersistentVolumeClaim, error) {
	// Initialize a list to hold associated PVCs.
	pvcList := &v1.PersistentVolumeClaimList{}

	// Loop through PVCs to find those associated with the specified PV.
	pvcNamespace := pv.Spec.ClaimRef.Namespace
	pvcList, err := clientset.CoreV1().PersistentVolumeClaims(pvcNamespace).List(context.TODO(), metav1.ListOptions{})
	if err != nil {
		return nil, err
	}

	associatedPVCs := make([]*v1.PersistentVolumeClaim, 0)

	for _, pvc := range pvcList.Items {
		if pvc.Spec.VolumeName == pv.Name {
			associatedPVCs = append(associatedPVCs, &pvc)
		}
	}

	return associatedPVCs, nil
}
