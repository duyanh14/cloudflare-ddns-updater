package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io/ioutil"
	"net/http"
	"strings"
	"time"

	"github.com/go-resty/resty/v2"
	"github.com/robfig/cron/v3"
)

var (
	authToken      string
	zoneIdentifier string
	recordName     string
)

type DNSRecord struct {
	ID      string `json:"id"`
	Content string `json:"content"`
}

type CloudflareResponse struct {
	Result   []DNSRecord `json:"result"`
	Success  bool        `json:"success"`
	Errors   []string    `json:"errors"`
	Messages []string    `json:"messages"`
}

func main() {
	flag.StringVar(&authToken, "authToken", "", "Cloudflare API Token")
	flag.StringVar(&zoneIdentifier, "zoneIdentifier", "", "Cloudflare Zone Identifier")
	flag.StringVar(&recordName, "recordName", "", "DNS Record Name")
	flag.Parse()

	if authToken == "" || zoneIdentifier == "" || recordName == "" {
		fmt.Println("Please provide all required parameters: -authToken, -zoneIdentifier, -recordName")
		return
	}

	err := runUpdate()
	if err != nil {
		fmt.Printf("First update error: %v\n", err)
	}

	c := cron.New()
	// Run the task every hour
	c.AddFunc("@hourly", func() {
		err := runUpdate()
		if err != nil {
			fmt.Printf("Update error: %v\n", err)
		}
	})

	c.Start()
	select {}
}

func runUpdate() error {
	fmt.Println("Check Initiated")

	ip, err := getExternalIP()
	if err != nil {
		return fmt.Errorf("Network error, cannot fetch external network IP: %v", err)
	}
	fmt.Printf("  > Fetched current external network IP: %s\n", ip)

	headerAuthParamHeader := make(map[string]string)
	if authToken != "" {
		headerAuthParamHeader["Authorization"] = "Bearer " + authToken
	}

	seekCurrentDNSValueURL := fmt.Sprintf("https://api.cloudflare.com/client/v4/zones/%s/dns_records?name=%s&type=A", zoneIdentifier, recordName)
	response, err := executeRequest("GET", seekCurrentDNSValueURL, nil, headerAuthParamHeader)
	if err != nil {
		return fmt.Errorf("Network error, cannot fetch DNS record: %v", err)
	}

	var cfResponse CloudflareResponse
	if err := json.Unmarshal(response, &cfResponse); err != nil {
		return fmt.Errorf("Error decoding JSON response: %v", err)
	}

	if !cfResponse.Success {
		fmt.Println("Error in Cloudflare API response.")
		return nil
	}

	// Ensure there is at least one record
	if len(cfResponse.Result) == 0 {
		fmt.Println("No DNS records found.")
		return nil
	}

	// We'll process the first record from the response
	record := cfResponse.Result[0]
	fmt.Printf("  > Fetched current DNS record value   : %s\n", record.Content)

	if ip == record.Content {
		fmt.Printf("Update for A record '%s' cancelled.\n  Reason: IP has not changed.\n", record.ID)
		return nil
	}

	fmt.Println("  > Different IP addresses detected, synchronizing...")

	jsonDataV4 := fmt.Sprintf(`{"id":"%s","type":"A","proxied":false,"name":"%s","content":"%s","ttl":120}`, zoneIdentifier, recordName, ip)
	updateURL := fmt.Sprintf("https://api.cloudflare.com/client/v4/zones/%s/dns_records/%s", zoneIdentifier, record.ID)

	_, err = executeRequest("PUT", updateURL, []byte(jsonDataV4), headerAuthParamHeader)
	if err != nil {
		return fmt.Errorf("Update error: %v", err)
	}

	fmt.Printf("Update for A record '%s' succeeded.\n  - Old value: %s\n  + New value: %s\n", record.ID, record.Content, ip)

	return nil
}

func getExternalIP() (string, error) {
	resp, err := http.Get("https://icanhazip.com/")
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	ipBytes, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}

	ip := strings.TrimSpace(string(ipBytes))
	return ip, nil
}

func executeRequest(method, url string, data []byte, headers map[string]string) ([]byte, error) {
	client := resty.New()
	client.SetTimeout(10 * time.Second)
	client.SetHeaders(headers)
	response, err := client.R().
		SetBody(data).
		Execute(method, url)
	if err != nil {
		return nil, err
	}
	return response.Body(), nil
}
