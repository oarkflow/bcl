package main

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"strconv"
	"sync"
	"time"

	"github.com/Juniper/go-netconf/netconf"
	"github.com/gosnmp/gosnmp"
	"golang.org/x/crypto/ssh"

	"github.com/oarkflow/bcl"
	"github.com/oarkflow/bcl/network/dsl"
)

// Helper: getPort returns an int port from extra, or a default if not found.
func getPort(extra map[string]interface{}, defaultPort int) int {
	if port, ok := extra["port"]; ok {
		switch v := port.(type) {
		case int:
			return v
		case float64:
			return int(v)
		case string:
			if p, err := strconv.Atoi(v); err == nil {
				return p
			}
		}
	}
	return defaultPort
}

// ----- Device Adapter Interface and Implementations ----- //

// DeviceAdapter is an interface for applying configurations to a device.
type DeviceAdapter interface {
	ApplyConfig(ctx context.Context, d *dsl.Device) error
}

// SSHAdapter applies configurations to devices via SSH.
type SSHAdapter struct{}

func (a *SSHAdapter) ApplyConfig(ctx context.Context, d *dsl.Device) error {
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

	// Use provided port (default 22)
	port := getPort(d.Extra, 22)

	config := &ssh.ClientConfig{
		User:            username,
		Auth:            []ssh.AuthMethod{ssh.Password(password)},
		HostKeyCallback: ssh.InsecureIgnoreHostKey(), // In production, verify host keys!
		Timeout:         5 * time.Second,
	}

	// Dial the SSH server.
	conn, err := ssh.Dial("tcp", fmt.Sprintf("%s:%d", ip, port), config)
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

	// Use provided port (default 443) for API calls.
	port := getPort(d.Extra, 443)
	url := fmt.Sprintf("https://%s:%d/api/config", ip, port)

	req, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewBuffer(payloadBytes))
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

// NETCONFAdapter applies configurations via NETCONF.
type NETCONFAdapter struct{}

func (a *NETCONFAdapter) ApplyConfig(ctx context.Context, d *dsl.Device) error {
	ip, ok := d.Extra["ip"]
	if !ok {
		return fmt.Errorf("NETCONF adapter: missing ip for device %s", d.Name)
	}
	username, ok := d.Extra["username"].(string)
	if !ok {
		return fmt.Errorf("NETCONF adapter: missing username for device %s", d.Name)
	}
	password, ok := d.Extra["password"].(string)
	if !ok {
		return fmt.Errorf("NETCONF adapter: missing password for device %s", d.Name)
	}
	// Use port (default NETCONF port 830)
	port := getPort(d.Extra, 830)
	target := fmt.Sprintf("%s:%d", ip, port)
	// Dial the NETCONF server via SSH
	sess, err := netconf.DialSSH(target, netconf.SSHConfigPassword(username, password))
	if err != nil {
		return fmt.Errorf("NETCONF dial failed for device %s: %v", d.Name, err)
	}
	defer sess.Close()
	// Send configuration as an XML RPC – for real usage the XML payload must match device schema.
	xmlConfig := fmt.Sprintf(`<config><hostname>%s</hostname></config>`, d.Name)
	reply, err := sess.Exec(netconf.RawMethod(xmlConfig))
	if err != nil {
		return fmt.Errorf("NETCONF exec failed for device %s: %v", d.Name, err)
	}
	log.Printf("NETCONF config applied on device %s, reply: %s", d.Name, reply.Data)
	return nil
}

// SNMPAdapter applies configurations via SNMP.
type SNMPAdapter struct{}

func (a *SNMPAdapter) ApplyConfig(ctx context.Context, d *dsl.Device) error {
	ipRaw, ok := d.Extra["ip"]
	if !ok {
		return fmt.Errorf("SNMP adapter: missing ip for device %s", d.Name)
	}
	ip := fmt.Sprintf("%v", ipRaw)
	community, ok := d.Extra["community"].(string)
	if !ok {
		return fmt.Errorf("SNMP adapter: missing community for device %s", d.Name)
	}
	port := getPort(d.Extra, 161)
	// Configure gosnmp
	snmp := &gosnmp.GoSNMP{
		Target:    ip,
		Port:      uint16(port),
		Community: community,
		Version:   gosnmp.Version2c,
		Timeout:   5 * time.Second,
		Retries:   1,
	}
	if err := snmp.Connect(); err != nil {
		return fmt.Errorf("SNMP connect failed for device %s: %v", d.Name, err)
	}
	defer snmp.Conn.Close()
	// Prepare SNMP SET payload.
	// Assume extra contains "snmp_oid" and "snmp_value" for the configuration.
	oidRaw, ok := d.Extra["snmp_oid"]
	if !ok {
		return fmt.Errorf("SNMP adapter: missing snmp_oid for device %s", d.Name)
	}
	oid := fmt.Sprintf("%v", oidRaw)
	value, ok := d.Extra["snmp_value"]
	if !ok {
		return fmt.Errorf("SNMP adapter: missing snmp_value for device %s", d.Name)
	}
	// Perform SNMP SET (using type OctetString as an example).
	pdus := []gosnmp.SnmpPDU{
		{
			Name:  oid,
			Type:  gosnmp.OctetString,
			Value: fmt.Sprintf("%v", value),
		},
	}
	result, err := snmp.Set(pdus)
	if err != nil {
		return fmt.Errorf("SNMP set failed for device %s: %v", d.Name, err)
	}
	log.Printf("SNMP config applied on device %s, result: %v", d.Name, result)
	return nil
}

// RESTCONFAdapter applies configurations via RESTCONF.
type RESTCONFAdapter struct{}

func (a *RESTCONFAdapter) ApplyConfig(ctx context.Context, d *dsl.Device) error {
	// Extract RESTCONF parameters.
	ip, ok := d.Extra["ip"]
	if !ok {
		return fmt.Errorf("RESTCONF adapter: missing ip for device %s", d.Name)
	}
	username, ok := d.Extra["username"].(string)
	if !ok {
		return fmt.Errorf("RESTCONF adapter: missing username for device %s", d.Name)
	}
	password, ok := d.Extra["password"].(string)
	if !ok {
		return fmt.Errorf("RESTCONF adapter: missing password for device %s", d.Name)
	}
	// Assume endpoint and port provided in extra.
	port := getPort(d.Extra, 443)
	endpoint := fmt.Sprintf("https://%s:%d/restconf/data/config", ip, port)
	// Generate JSON payload from device config.
	payload, err := generateAPIPayloadForDevice(d)
	if err != nil {
		return fmt.Errorf("RESTCONF payload generation failed for device %s: %v", d.Name, err)
	}
	// Create HTTP request with basic auth.
	req, err := http.NewRequestWithContext(ctx, "PUT", endpoint, bytes.NewBuffer(payload))
	if err != nil {
		return fmt.Errorf("RESTCONF request creation failed for device %s: %v", d.Name, err)
	}
	req.SetBasicAuth(username, password)
	req.Header.Set("Content-Type", "application/yang-data+json")
	// Configure TLS (in production verify certificates).
	tr := &http.Transport{
		TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
	}
	client := &http.Client{
		Timeout:   5 * time.Second,
		Transport: tr,
	}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("RESTCONF request failed for device %s: %v", d.Name, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("RESTCONF config failed for device %s: status %d, body: %s", d.Name, resp.StatusCode, string(body))
	}
	log.Printf("RESTCONF config applied on device %s", d.Name)
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
	case "netconf":
		return &NETCONFAdapter{}
	case "snmp":
		return &SNMPAdapter{}
	case "restconf":
		return &RESTCONFAdapter{}
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
