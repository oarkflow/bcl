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

type DeviceAdapter interface {
	ApplyConfig(ctx context.Context, d *dsl.Device) error
}

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
	port := getPort(d.Extra, 22)
	config := &ssh.ClientConfig{
		User:            username,
		Auth:            []ssh.AuthMethod{ssh.Password(password)},
		HostKeyCallback: ssh.InsecureIgnoreHostKey(),
		Timeout:         5 * time.Second,
	}
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
	command := generateCommandForDevice(d)
	var output bytes.Buffer
	session.Stdout = &output
	if err := session.Run(command); err != nil {
		return fmt.Errorf("SSH run command failed for device %s: %v", d.Name, err)
	}
	log.Printf("SSH config applied on device %s, output: %s", d.Name, output.String())
	return nil
}

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
	payloadBytes, err := generateAPIPayloadForDevice(d)
	if err != nil {
		return fmt.Errorf("API payload generation failed for device %s: %v", d.Name, err)
	}
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
	oidRaw, ok := d.Extra["snmp_oid"]
	if !ok {
		return fmt.Errorf("SNMP adapter: missing snmp_oid for device %s", d.Name)
	}
	oid := fmt.Sprintf("%v", oidRaw)
	value, ok := d.Extra["snmp_value"]
	if !ok {
		return fmt.Errorf("SNMP adapter: missing snmp_value for device %s", d.Name)
	}
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

type RESTCONFAdapter struct{}

func (a *RESTCONFAdapter) ApplyConfig(ctx context.Context, d *dsl.Device) error {
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
	port := getPort(d.Extra, 443)
	endpoint := fmt.Sprintf("https://%s:%d/restconf/data/config", ip, port)
	payload, err := generateAPIPayloadForDevice(d)
	if err != nil {
		return fmt.Errorf("RESTCONF payload generation failed for device %s: %v", d.Name, err)
	}
	req, err := http.NewRequestWithContext(ctx, "PUT", endpoint, bytes.NewBuffer(payload))
	if err != nil {
		return fmt.Errorf("RESTCONF request creation failed for device %s: %v", d.Name, err)
	}
	req.SetBasicAuth(username, password)
	req.Header.Set("Content-Type", "application/yang-data+json")
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

	port := getPort(d.Extra, 830)
	target := fmt.Sprintf("%s:%d", ip, port)

	sshConfig := &ssh.ClientConfig{
		User:            username,
		Auth:            []ssh.AuthMethod{ssh.Password(password)},
		HostKeyCallback: ssh.InsecureIgnoreHostKey(),
		Timeout:         5 * time.Second,
	}

	session, err := netconf.DialSSH(target, sshConfig)
	if err != nil {
		return fmt.Errorf("NETCONF dial failed for device %s: %v", d.Name, err)
	}
	defer session.Close()

	configData, err := generateNETCONFPayloadForDevice(d)
	if err != nil {
		return fmt.Errorf("NETCONF payload generation failed for device %s: %v", d.Name, err)
	}

	rpcPayload := fmt.Sprintf(`<?xml version="1.0" encoding="UTF-8"?>
<rpc message-id="101" xmlns="urn:ietf:params:xml:ns:netconf:base:1.0">
  <edit-config>
    <target>
      <running/>
    </target>
    <config>
      %s
    </config>
  </edit-config>
</rpc>`, configData)

	reply, err := session.Exec(netconf.RawMethod(rpcPayload))
	if err != nil {
		return fmt.Errorf("NETCONF edit-config failed for device %s: %v", d.Name, err)
	}
	log.Printf("NETCONF config applied on device %s, reply: %v", d.Name, reply)
	return nil
}

func getDeviceAdapter(d *dsl.Device) DeviceAdapter {
	method, ok := d.Extra["connection_method"]
	if !ok {
		method = "ssh"
	}
	switch method {
	case "ssh":
		return &SSHAdapter{}
	case "api":
		return &APIAdapter{}
	case "snmp":
		return &SNMPAdapter{}
	case "restconf":
		return &RESTCONFAdapter{}
	case "netconf":
		return &NETCONFAdapter{}
	default:
		return &SSHAdapter{}
	}
}

func generateCommandForDevice(d *dsl.Device) string {
	cmd := fmt.Sprintf("configure terminal\nhostname %s\n", d.Name)
	for ifaceName, iface := range d.Interfaces {
		cmd += fmt.Sprintf("interface %s\n", ifaceName)
		if iface.IP != "" {
			cmd += fmt.Sprintf("ip address %s\n", iface.IP)
		}
		cmd += "no shutdown\nexit\n"
	}
	cmd += "end\nwrite memory\n"
	return cmd
}

func generateAPIPayloadForDevice(d *dsl.Device) ([]byte, error) {
	payload := map[string]interface{}{
		"device": d.Name,
		"type":   d.Type,
		"config": d.Interfaces,
	}
	return json.Marshal(payload)
}

func generateNETCONFPayloadForDevice(d *dsl.Device) (string, error) {
	xmlPayload := `<config>
  <device xmlns="http://example.com/device">
    <name>` + d.Name + `</name>
    <interfaces>`
	for ifaceName, iface := range d.Interfaces {
		xmlPayload += `
      <interface>
        <name>` + ifaceName + `</name>`
		if iface.IP != "" {
			xmlPayload += `
        <ipAddress>` + iface.IP + `</ipAddress>`
		}
		xmlPayload += `
      </interface>`
	}
	xmlPayload += `
    </interfaces>
  </device>
</config>`
	return xmlPayload, nil
}

func applyConfigurations(net *dsl.Network) {
	var wg sync.WaitGroup
	for i := range net.Devices {
		wg.Add(1)
		d := &net.Devices[i]
		go func(dev *dsl.Device) {
			defer wg.Done()
			adapter := getDeviceAdapter(dev)
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
		applyConfigurations(&netConfig)
		log.Println("Configuration application complete.")
	}
}
