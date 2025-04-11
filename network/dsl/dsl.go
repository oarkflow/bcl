package dsl

type NetworkConfig struct {
	Protocols []Protocol `json:"protocol"`
	Networks  []Network  `json:"network"`
}

// Protocol defines the global protocol with its parameters.
type Protocol struct {
	// Name of the protocol e.g., "OSPF", "BGP", "VLAN", "MPLS".
	Name string `json:"name"`
	// Parameters holds protocol-specific settings (e.g., helloInterval, deadInterval).
	Parameters map[string]any `json:"parameters"`
}

// Interface represents a network device interface.
type Interface struct {
	// Name of the interface (e.g., "eth0", "port1").
	Name string `json:"name"`
	// IP address configured on the interface. May be empty for non-IP interfaces.
	IP string `json:"ip,omitempty"`
	// Protocol to be used on this interface.
	Protocol string `json:"protocol"`
	// Extra allows additional properties such as VLAN ID, speed, etc.
	Extra map[string]string `json:"extra,omitempty"`
}

// Automation defines an automation trigger block for dynamic configuration tasks.
type Automation struct {
	// Trigger is the event name (e.g., "interfaceDown", "latencyThresholdExceeded").
	Trigger string `json:"trigger"`
	// Condition is an optional expression to filter when the trigger should activate.
	Condition string `json:"condition"`
	// Action is the command or procedure to execute if the trigger fires.
	Action string `json:"action"`
}

// Device represents a network device with a type, interfaces, and optional automation.
type Device struct {
	// Name is a unique identifier for the device.
	Name string `json:"name"`
	// Type indicates the type of device such as "Router", "Switch", "Gateway", "Firewall", etc.
	Type string `json:"type"`
	// Interfaces is a map of interface names to their configuration.
	Interfaces map[string]Interface `json:"interfaces"`
	// Automation, if provided, defines device-specific automation.
	Automation *Automation `json:"automation,omitempty"`
	// Extra holds additional properties for device connection, such as management IP,
	// credentials (username, password), connection method (ssh, api), etc.
	Extra map[string]any `json:"extra,omitempty"`
}

// Connection models the link between two device interfaces.
type Connection struct {
	// Name is a unique identifier for the connection.
	Name string `json:"name"`
	// From specifies the source in a "Device.Interface" format.
	From any `json:"from"`
	// To specifies the destination in a "Device.Interface" format.
	To any `json:"to"`
	// Parameters holds additional connection properties such as bandwidth, latency, VLAN, etc.
	Parameters map[string]any `json:"parameters"`
}

// SSHConfig holds SSH-specific connection information.
type SSHConfig struct {
	IP       string `json:"ip"`
	Port     int    `json:"port,omitempty"` // Defaults to 22 if not provided.
	Username string `json:"username"`
	Password string `json:"password"`
}

// APIConfig holds API-specific connection information.
type APIConfig struct {
	Endpoint string `json:"endpoint"` // e.g., "https://10.0.0.1/api/config"
	Token    string `json:"token"`
}

// AccessConfig aggregates the access method for devices.
// Method is "ssh" or "api" and the corresponding field is populated.
type AccessConfig struct {
	Method string     `json:"method"`
	SSH    *SSHConfig `json:"ssh,omitempty"`
	API    *APIConfig `json:"api,omitempty"`
}

// Network aggregates all configuration elements to represent an entire enterprise network.
type Network struct {
	// Name of the network.
	Name string `json:"name"`
	// Protocols is a map from protocol names to their definitions.
	Protocols map[string]Protocol `json:"protocols"`
	// Devices is a map from device names to device configurations.
	Devices []Device `json:"device"`
	// Connections is a map from connection names to connection configurations.
	Connections []Connection `json:"connection"`
	// Automations holds any network-level automation definitions.
	Automations []Automation `json:"automation,omitempty"`
}
