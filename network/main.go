package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"sync"
	"time"

	"golang.org/x/crypto/ssh"

	"github.com/oarkflow/bcl"
	"github.com/oarkflow/bcl/network/dsl"
)

// ----- Device Adapter Interface and Implementations ----- //

// DeviceAdapter is an interface for applying configurations to a device.
type DeviceAdapter interface {
	ApplyConfig(ctx context.Context, d *dsl.Device) error
}

// SSHAdapter applies configurations to devices via SSH.
type SSHAdapter struct{}

func (a *SSHAdapter) ApplyConfig(ctx context.Context, d *dsl.Device) error {
	// Read required connection parameters from d.Extra.
	ip, ok := d.Extra["ip"]
	if !ok {
		return fmt.Errorf("SSH adapter: missing ip for device %s", d.Name)
	}
	username, ok := d.Extra["username"].(string)
	if !ok {
		return fmt.Errorf("SSH adapter: missing username for device %s", d.Name)
	}
	password, ok := d.Extra["password"].(string)
	if !ok {
		return fmt.Errorf("SSH adapter: missing password for device %s", d.Name)
	}

	config := &ssh.ClientConfig{
		User:            username,
		Auth:            []ssh.AuthMethod{ssh.Password(password)},
		HostKeyCallback: ssh.InsecureIgnoreHostKey(), // In production, verify host keys!
		Timeout:         5 * time.Second,
	}

	// Dial the SSH server.
	conn, err := ssh.Dial("tcp", fmt.Sprintf("%s:22", ip), config)
	if err != nil {
		return fmt.Errorf("SSH dial failed for device %s: %v", d.Name, err)
	}
	defer conn.Close()

	session, err := conn.NewSession()
	if err != nil {
		return fmt.Errorf("SSH new session failed for device %s: %v", d.Name, err)
	}
	defer session.Close()

	// Generate command(s) for this device. In production, use text/template for generating commands.
	command := generateCommandForDevice(d)

	// Run the command and capture output.
	var output bytes.Buffer
	session.Stdout = &output
	if err := session.Run(command); err != nil {
		return fmt.Errorf("SSH run command failed for device %s: %v", d.Name, err)
	}
	log.Printf("SSH config applied on device %s, output: %s", d.Name, output.String())
	return nil
}

// APIAdapter applies configurations to devices via an HTTP API.
type APIAdapter struct{}

func (a *APIAdapter) ApplyConfig(ctx context.Context, d *dsl.Device) error {
	ip, ok := d.Extra["ip"]
	if !ok {
		return fmt.Errorf("API adapter: missing ip for device %s", d.Name)
	}
	token, ok := d.Extra["api_token"].(string)
	if !ok {
		return fmt.Errorf("API adapter: missing api_token for device %s", d.Name)
	}

	// Generate a JSON payload from the device configuration.
	payloadBytes, err := generateAPIPayloadForDevice(d)
	if err != nil {
		return fmt.Errorf("API payload generation failed for device %s: %v", d.Name, err)
	}
	req, err := http.NewRequestWithContext(ctx, "POST", fmt.Sprintf("https://%s/api/config", ip), bytes.NewBuffer(payloadBytes))
	if err != nil {
		return fmt.Errorf("API new request failed for device %s: %v", d.Name, err)
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")

	client := &http.Client{
		Timeout: 5 * time.Second,
	}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("API request failed for device %s: %v", d.Name, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("API config failed for device %s: status %d, body: %s", d.Name, resp.StatusCode, string(body))
	}
	log.Printf("API config applied on device %s", d.Name)
	return nil
}

// getDeviceAdapter returns an adapter based on the device's connection method.
// You can expand this as needed (for example, adding NETCONF, SNMP, etc.).
func getDeviceAdapter(d *dsl.Device) DeviceAdapter {
	method, ok := d.Extra["connection_method"]
	if !ok {
		method = "ssh" // default to SSH if not specified.
	}
	switch method {
	case "ssh":
		return &SSHAdapter{}
	case "api":
		return &APIAdapter{}
	default:
		return &SSHAdapter{}
	}
}

// ----- Command and Payload Generation Helpers ----- //

// generateCommandForDevice creates a device-specific configuration command string.
// In production, this would likely be generated using a templating system.
func generateCommandForDevice(d *dsl.Device) string {
	cmd := fmt.Sprintf("configure terminal\nhostname %s\n", d.Name)
	for ifaceName, iface := range d.Interfaces {
		cmd += fmt.Sprintf("interface %s\n", ifaceName)
		if iface.IP != "" {
			cmd += fmt.Sprintf("ip address %s\n", iface.IP)
		}
		// Additional commands based on protocol and extra parameters can be added here.
		cmd += fmt.Sprintf("no shutdown\nexit\n")
	}
	cmd += "end\nwrite memory\n"
	return cmd
}

// generateAPIPayloadForDevice creates a JSON payload from the device configuration.
func generateAPIPayloadForDevice(d *dsl.Device) ([]byte, error) {
	// In production, use a well-defined struct that represents the device config.
	payload := map[string]interface{}{
		"device": d.Name,
		"type":   d.Type,
		"config": d.Interfaces, // This is a simplified example.
	}
	return json.Marshal(payload)
}

// ----- Configuration Application Function ----- //

// applyConfigurations applies the configuration concurrently to all devices.
func applyConfigurations(net *dsl.Network) {
	var wg sync.WaitGroup
	// Iterate using index to safely take a pointer to each device.
	for i := range net.Devices {
		wg.Add(1)
		d := &net.Devices[i]
		go func(dev *dsl.Device) {
			defer wg.Done()
			adapter := getDeviceAdapter(dev)
			// Create a context with a timeout for each device configuration.
			ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
			defer cancel()
			if err := adapter.ApplyConfig(ctx, dev); err != nil {
				log.Printf("Failed to apply config on device %s: %v", dev.Name, err)
			} else {
				log.Printf("Successfully applied config on device %s", dev.Name)
			}
		}(d)
	}
	wg.Wait()
}

// ----- Main Function ----- //

func main() {
	input, err := os.ReadFile("network.bcl")
	if err != nil {
		panic(err)
	}
	var config dsl.NetworkConfig
	_, err = bcl.Unmarshal([]byte(input), &config)
	if err != nil {
		panic(err)
	}
	fmt.Println("Unmarshalled Config:")
	fmt.Printf("%+v\n\n", config)
	for _, netConfig := range config.Networks {
		log.Printf("Starting configuration application for network: %s", netConfig.Name)

		// Apply the configuration to all devices concurrently.
		applyConfigurations(&netConfig)

		log.Println("Configuration application complete.")
	}

}
