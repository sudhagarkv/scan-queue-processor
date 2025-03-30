package scm_service

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"

	"github.com/Azure/azure-sdk-for-go/sdk/security/keyvault/azkeys"

	"scan-queue-processor/constants"
	"scan-queue-processor/models"
)

type SCMService interface {
	GetRepoSize(ctx context.Context, scanRequest *models.QueuedScan) (float64, error)
}

type scm struct {
	client    http.Client
	token     string
	keyClient *azkeys.Client
}

func (s scm) GetRepoSize(ctx context.Context, scanRequest *models.QueuedScan) (float64, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, fmt.Sprintf("https://api.github.com/repos/%s/%s", scanRequest.Namespace, scanRequest.RepoName), nil)
	if err != nil {
		log.Printf("Unable to create request to get repo details %v", err)
		return 0, err
	}

	request.Header.Add("Authorization", "Bearer "+s.token)

	version := ""
	algorithmRSAOAEP := azkeys.EncryptionAlgorithmRSAOAEP256
	if scanRequest.IsPrivate {
		decrypt, err := s.keyClient.Decrypt(ctx, constants.TokenKey, version, azkeys.KeyOperationParameters{
			Algorithm: &algorithmRSAOAEP,
			Value:     []byte(scanRequest.EncryptedToken),
		}, nil)
		if err != nil {
			log.Printf("Unable to decrypt token using token key %v", err)
			return 0, err
		}
		request.Header.Add("Authorization", "Bearer "+string(decrypt.Result))
	}

	response, err := s.client.Do(request)
	if err != nil {
		log.Printf("Unable to make http request to get repo details %v", err)
		return 0, err
	}

	defer response.Body.Close()
	var repo models.GithubRepo
	bodyBytes, err := io.ReadAll(response.Body)
	if err != nil {
		log.Printf("Unable to read body %v", err)
		return 0, err
	}
	if response.StatusCode != 200 {
		return 0, fmt.Errorf("unable to get repo size error: %v", string(bodyBytes))
	}

	err = json.Unmarshal(bodyBytes, &repo)
	if err != nil {
		log.Printf("Unable to decode repo response %v", err)
		return 0, err
	}

	return repo.Size, nil
}

func NewGithub(client http.Client, keysClient *azkeys.Client, apiToken string) SCMService {
	return &scm{
		client:    client,
		token:     apiToken,
		keyClient: keysClient,
	}
}
