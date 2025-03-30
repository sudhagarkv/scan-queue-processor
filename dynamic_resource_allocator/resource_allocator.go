package dynamic_resource_allocator

import (
	"context"
	"database/sql"
	"fmt"
	"log"
	"strconv"
	"strings"

	"github.com/google/uuid"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"

	"scan-queue-processor/models"
	"scan-queue-processor/scm_service"
)

type Manager interface {
	GetStorageInformation() ([]models.ResourceInfo, error)
	ProcessRequest(ctx context.Context, scanRequest models.QueuedScan) error
}

type ResourceManager struct {
	k8Client  *kubernetes.Clientset
	scmClient scm_service.SCMService
	namespace string
	db        *sql.DB
}

func isPVCReady(pvc *v1.PersistentVolumeClaim) bool {
	// Check if the PVC is in the Bound or Available phase.
	// You can also check for specific conditions if needed.
	return pvc.Status.Phase == v1.ClaimBound
}

func (rm *ResourceManager) ProcessRequest(ctx context.Context, scanRequest models.QueuedScan) error {
	uniqueIdentifier := uuid.NewString()
	pvName := "trufflehog-pv-" + uniqueIdentifier

	size, err := rm.scmClient.GetRepoSize(ctx, &scanRequest)
	if err != nil {
		return err
	}

	className := "manual"
	request, addition, unit := convertToMiGi(size)
	actual := request + addition
	requestSize := fmt.Sprintf("%d%s", int(request), unit)
	actualSize := fmt.Sprintf("%d%s", int(actual), unit)
	filesystem := v1.PersistentVolumeFilesystem

	pv := &v1.PersistentVolume{
		ObjectMeta: metav1.ObjectMeta{
			Name: pvName,
		},
		Spec: v1.PersistentVolumeSpec{
			Capacity: v1.ResourceList{
				v1.ResourceStorage: resource.MustParse(actualSize),
			},
			VolumeMode:       &filesystem,
			StorageClassName: className,
			AccessModes:      []v1.PersistentVolumeAccessMode{v1.ReadWriteMany},
			PersistentVolumeSource: v1.PersistentVolumeSource{
				HostPath: &v1.HostPathVolumeSource{
					Path: "/tmp/trufflehog/" + scanRequest.RepoName, // Replace with your desired path
				},
			},
			PersistentVolumeReclaimPolicy: v1.PersistentVolumeReclaimDelete,
		},
	}

	log.Println("Creating PersistentVolume...")
	_, err = rm.k8Client.CoreV1().PersistentVolumes().Create(context.Background(), pv, metav1.CreateOptions{})
	if err != nil {
		panic(err.Error())
	}

	pvcName := "trufflehog-pvc-" + uniqueIdentifier
	podName := strings.ReplaceAll(strings.ToLower(scanRequest.RepoName), "_", "-") + "-" + uniqueIdentifier

	if len(podName) > 63 {
		podName = podName[:62]
	}

	// Define the pod configuration.
	pod := &v1.Pod{
		TypeMeta: metav1.TypeMeta{
			Kind:       "Pod",
			APIVersion: "v1",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name: podName,
		},
		Spec: v1.PodSpec{
			ImagePullSecrets: []v1.LocalObjectReference{
				{
					Name: "artifactory-registry",
				},
			},
			Containers: []v1.Container{
				{
					ImagePullPolicy: v1.PullAlways,
					Name:            podName,
					Image:           "sudhagarkv/truffle-scanner:latest", // Replace with your image name and tag.
					Env: []v1.EnvVar{
						{
							Name:  "URL",
							Value: scanRequest.URL,
						},
						{
							Name:  "ENCRYPTED_TOKEN",
							Value: scanRequest.EncryptedToken,
						},
						{
							Name:  "IS_PRIVATE",
							Value: strconv.FormatBool(scanRequest.IsPrivate),
						},
						{
							Name:  "SCAN_ID",
							Value: uniqueIdentifier,
						},
						{
							Name: "DB_HOST",
							ValueFrom: &v1.EnvVarSource{
								SecretKeyRef: &v1.SecretKeySelector{
									LocalObjectReference: v1.LocalObjectReference{
										Name: "global-secret",
									},
									Key: "DB_HOST",
								},
							},
						},
						{
							Name: "DB_PORT",
							ValueFrom: &v1.EnvVarSource{
								SecretKeyRef: &v1.SecretKeySelector{
									LocalObjectReference: v1.LocalObjectReference{
										Name: "global-secret",
									},
									Key: "DB_PORT",
								},
							},
						},
						{
							Name: "DB_NAME",
							ValueFrom: &v1.EnvVarSource{
								SecretKeyRef: &v1.SecretKeySelector{
									LocalObjectReference: v1.LocalObjectReference{
										Name: "global-secret",
									},
									Key: "DB_NAME",
								},
							},
						},
						{
							Name: "DB_USER",
							ValueFrom: &v1.EnvVarSource{
								SecretKeyRef: &v1.SecretKeySelector{
									LocalObjectReference: v1.LocalObjectReference{
										Name: "global-secret",
									},
									Key: "DB_USER",
								},
							},
						},
						{
							Name: "AZURE_CLIENT_ID",
							ValueFrom: &v1.EnvVarSource{
								SecretKeyRef: &v1.SecretKeySelector{
									LocalObjectReference: v1.LocalObjectReference{
										Name: "global-secret",
									},
									Key: "AZURE_CLIENT_ID",
								},
							},
						},
						{
							Name: "AZURE_TENANT_ID",
							ValueFrom: &v1.EnvVarSource{
								SecretKeyRef: &v1.SecretKeySelector{
									LocalObjectReference: v1.LocalObjectReference{
										Name: "global-secret",
									},
									Key: "AZURE_TENANT_ID",
								},
							},
						},
						{
							Name: "AZURE_CLIENT_SECRET",
							ValueFrom: &v1.EnvVarSource{
								SecretKeyRef: &v1.SecretKeySelector{
									LocalObjectReference: v1.LocalObjectReference{
										Name: "global-secret",
									},
									Key: "AZURE_CLIENT_SECRET",
								},
							},
						},
						{
							Name: "KEY_VAULT_URL",
							ValueFrom: &v1.EnvVarSource{
								SecretKeyRef: &v1.SecretKeySelector{
									LocalObjectReference: v1.LocalObjectReference{
										Name: "global-secret",
									},
									Key: "KEY_VAULT_URL",
								},
							},
						},
					},
					Resources: v1.ResourceRequirements{
						Requests: v1.ResourceList{
							v1.ResourceCPU:    resource.MustParse("0.5"),   // CPU request.
							v1.ResourceMemory: resource.MustParse("256Mi"), // Memory request.
						},
						Limits: v1.ResourceList{
							v1.ResourceCPU:    resource.MustParse("3"),   // CPU limit.
							v1.ResourceMemory: resource.MustParse("1Gi"), // Memory limit.
						},
					},
					VolumeMounts: []v1.VolumeMount{
						{
							Name:      "scan-volume",
							MountPath: "/home/scanner/",
						},
					},
				},
			},
			Volumes: []v1.Volume{
				{
					Name: "scan-volume",
					VolumeSource: v1.VolumeSource{
						PersistentVolumeClaim: &v1.PersistentVolumeClaimVolumeSource{
							ClaimName: pvcName,
						},
					},
				},
			},
		},
	}

	createdPod, err := rm.k8Client.CoreV1().Pods(rm.namespace).Create(context.TODO(), pod, metav1.CreateOptions{})
	if err != nil {
		panic(err.Error())
	}

	log.Printf("Pod %s with id %s created in namespace %s\n", createdPod.Name, createdPod.UID, rm.namespace)

	pvc := &v1.PersistentVolumeClaim{
		ObjectMeta: metav1.ObjectMeta{
			Name:      pvcName,
			Namespace: rm.namespace,
			OwnerReferences: []metav1.OwnerReference{
				{
					APIVersion: "v1",
					Kind:       "Pod",
					Name:       createdPod.Name,
					UID:        createdPod.UID,
				},
			},
		},
		Spec: v1.PersistentVolumeClaimSpec{
			AccessModes: []v1.PersistentVolumeAccessMode{v1.ReadWriteMany},
			Resources: v1.ResourceRequirements{
				Requests: v1.ResourceList{
					v1.ResourceStorage: resource.MustParse(requestSize),
				},
			},
			StorageClassName: &className,
			VolumeMode:       &filesystem,
		},
	}

	_, err = rm.k8Client.CoreV1().PersistentVolumeClaims(rm.namespace).Create(context.TODO(), pvc, metav1.CreateOptions{})
	if err != nil {
		panic(err.Error())
	}
	log.Printf("Created PVC %s\n", pvcName)

	_, err = rm.db.ExecContext(ctx, `UPDATE scan_requests SET queue_status = $1, modified_at = current_timestamp, scan_id = $2 WHERE id = $3`, "Scheduled", uniqueIdentifier, scanRequest.ID)
	if err != nil {
		log.Printf("Unable to update queue status %v", err)
		return err
	}

	log.Println("Updated queue status to queued for scan_id ", uniqueIdentifier)

	return nil
}

func convertToMiGi(kb float64) (float64, float64, string) {
	if kb >= 1024*1024 {
		return kb / (1024 * 1024), 1, "Gi"
	} else if kb >= 1024 {
		return kb, 100, "Mi"
	}
	return kb, 1000, "Ki"
}

func NewResourceManager(client *kubernetes.Clientset, scmService scm_service.SCMService, db *sql.DB, namespace string) *ResourceManager {
	return &ResourceManager{
		k8Client:  client,
		scmClient: scmService,
		namespace: namespace,
		db:        db,
	}
}
