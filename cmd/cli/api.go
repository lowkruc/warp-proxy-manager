package main

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
)

func getBaseURL() string {
	host := os.Getenv("WARP_MANAGER_HOST")
	if host == "" {
		host = "localhost"
	}
	port := os.Getenv("WARP_MANAGER_PORT")
	if port == "" {
		port = readPortFromConfig()
	}
	return fmt.Sprintf("http://%s:%s", host, port)
}

func readPortFromConfig() string {
	dir := findInstallDir()
	if dir == "" {
		return "8080"
	}

	data, err := os.ReadFile(filepath.Join(dir, "config.yaml"))
	if err != nil {
		return "8080"
	}

	lines := strings.Split(string(data), "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "api_port:") {
			return strings.TrimSpace(strings.TrimPrefix(line, "api_port:"))
		}
	}
	return "8080"
}

func apiGet(path string) ([]byte, error) {
	url := getBaseURL() + path
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, err
	}

	token := os.Getenv("WARP_TOKEN")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("API error %d: %s", resp.StatusCode, string(body))
	}

	return body, nil
}

func apiPost(path string, body interface{}) ([]byte, error) {
	url := getBaseURL() + path

	var reqBody io.Reader
	if body != nil {
		jsonBytes, _ := json.Marshal(body)
		reqBody = strings.NewReader(string(jsonBytes))
	}

	req, err := http.NewRequest("POST", url, reqBody)
	if err != nil {
		return nil, err
	}

	token := os.Getenv("WARP_TOKEN")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("API error %d: %s", resp.StatusCode, string(respBody))
	}

	return respBody, nil
}

func apiDelete(path string) error {
	url := getBaseURL() + path
	req, err := http.NewRequest("DELETE", url, nil)
	if err != nil {
		return err
	}

	token := os.Getenv("WARP_TOKEN")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("API error %d: %s", resp.StatusCode, string(body))
	}

	return nil
}
