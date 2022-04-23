/*
Copyright 2021 Wim Henderickx.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package v1

import (
	nddv1 "github.com/yndd/ndd-runtime/apis/common/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
)

/*
// Protocol represents the protocol used to connect to the target
type Protocol string

const (
	// ProtocolGnmi operates using the gnmi specification
	ProtocolGnmi Protocol = "gnmi"

	// ProtocolNetconf operates using the netconf specification
	ProtocolNetconf Protocol = "netconf"
)

// Config contains the information necessary to communicate with
// the target.
type Config struct {
	// protocol defines the protocol to connect to the target
	// +optional
	// +kubebuilder:validation:Enum=`gnmi`;`netconf`
	// +kubebuilder:default=gnmi
	Protocol *Protocol `json:"protocol,omitempty"`

	// Address holds the IP:port for accessing the target
	// +kubebuilder:validation:Required
	Address *string `json:"address"`

	// Proxy used to communicate to the target
	// +kubebuilder:validation:Optional
	Proxy *string `json:"proxy,omitempty"`

	// The name of the secret containing the credentials (requires
	// keys "username" and "password").
	// +kubebuilder:validation:Required
	CredentialsName *string `json:"credentialsName"`

	// The name of the secret containing the credentials (requires
	// keys "TLSCA" and "TLSCert", " TLSKey").
	// +kubebuilder:validation:Optional
	TLSCredentialsName *string `json:"tlsCredentialsName,omitempty"`

	// SkipVerify disables verification of server certificates when using
	// HTTPS to connect to the Target. This is required when the server
	// certificate is self-signed, but is insecure because it allows a
	// man-in-the-middle to intercept the connection.
	// +kubebuilder:default:=false
	// +kubebuilder:validation:Optional
	SkipVerify *bool `json:"skpVerify,omitempty"`

	// Insecure runs the communication in an insecure manner
	// +kubebuilder:default:=false
	// +kubebuilder:validation:Optional
	Insecure *bool `json:"insecure,omitempty"`

	// Encoding defines the gnmi encoding
	// +kubebuilder:validation:Enum=`JSON`;`BYTES`;`PROTO`;`ASCII`;`JSON_IETF`
	// +kubebuilder:default=JSON_IETF
	// +kubebuilder:validation:Optional
	Encoding *string `json:"encoding,omitempty"`
}
*/

// DiscoveryInfo collects information about the target
type DiscoveryInfo struct {
	// the VendorType of the target
	VendorType *string `json:"vendor-type,omitempty"`

	// Host name of the target
	HostName *string `json:"hostname,omitempty"`

	// the Kind of the target
	Kind *string `json:"kind,omitempty"`

	// SW version of the target
	SwVersion *string `json:"sw-version,omitempty"`

	// the Mac address of the target
	MacAddress *string `json:"mac-address,omitempty"`

	// the Serial Number of the target
	SerialNumber *string `json:"serial-number,omitempty"`

	// Supported Encodings of the target
	SupportedEncodings []string `json:"supported-encodings,omitempty"`
}

// Status defines the observed state of the Target
type Status struct {
	// The DiscoveryInfo of the Target
	DiscoveryInfo *DiscoveryInfo `json:"discoveryInfo,omitempty"`

	// UsedTargetSpec identifies the used target spec when installed
	// UsedTargetSpec *TargetSpec `json:"usedTargetSpec,omitempty"`
}

// TargetSpec defines the desired spec of target
type TargetSpec struct {
	// Contains all fields for the specific resource being addressed
	//+kubebuilder:pruning:PreserveUnknownFields
	//+kubebuilder:validation:Required
	Properties runtime.RawExtension `json:"properties,omitempty"`

	// Config defines the details how we connect to the target
	//Config *Config `json:"config"`

	// VendorType defines the vendor type of the target
	// +optional
	//VendorType *VendorType `json:"vendorType,omitempty"`
}

// TargetStatus defines the observed state of TargetNode
type TargetStatus struct {
	// identifies the status of the reconciliation process
	nddv1.ConditionedStatus `json:",inline"`
	// identifies the controller reference of the target
	ControllerRef           nddv1.Reference `json:"controllerRef,omitempty"`
	// identifies the operational information of the target
	Status                  `json:",inline"`
}

// +kubebuilder:object:root=true
// +genclient
// +genclient:nonNamespaced

// Target is the Schema for the targets API
// +kubebuilder:subresource:status
// +kubebuilder:printcolumn:name="SYNC",type="string",JSONPath=".status.conditions[?(@.kind=='Synced')].status"
// +kubebuilder:printcolumn:name="READY",type="string",JSONPath=".status.conditions[?(@.kind=='Ready')].status"
// +kubebuilder:printcolumn:name="ADDRESS",type="string",JSONPath=".spec.target.address",description="address to connect to the target'"
// +kubebuilder:printcolumn:name="PROTOCOL",type="string",JSONPath=".spec.target.protocol",description="Protocol used to communicate to the target"
// +kubebuilder:printcolumn:name="VENDORTYPE",type="string",JSONPath=".status.discoveryInfo.vendorType",description="VendorType of target"
// +kubebuilder:printcolumn:name="KIND",type="string",JSONPath=".status.discoveryInfo.kind",description="Kind of target"
// +kubebuilder:printcolumn:name="SWVERSION",type="string",JSONPath=".status.discoveryInfo.swVersion",description="SW version of the target"
// +kubebuilder:printcolumn:name="MACADDRESS",type="string",JSONPath=".status.discoveryInfo.macAddress",description="macAddress of the target"
// +kubebuilder:printcolumn:name="SERIALNBR",type="string",JSONPath=".status.discoveryInfo.serialNumber",description="serialNumber of the target"
// +kubebuilder:printcolumn:name="AGE",type="date",JSONPath=".metadata.creationTimestamp"
// +kubebuilder:resource:categories={ndd,nddd}, shortName=t
type Target struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   TargetSpec   `json:"spec,omitempty"`
	Status TargetStatus `json:"status,omitempty"`
}

//+kubebuilder:object:root=true

// TargetList contains a list of targets
type TargetList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []Target `json:"items"`
}

// +kubebuilder:object:root=true

// A TargetUsage indicates that a resource is using a target.
// +kubebuilder:printcolumn:name="AGE",type="date",JSONPath=".metadata.creationTimestamp"
// +kubebuilder:printcolumn:name="CONFIG-NAME",type="string",JSONPath=".TargeteRef.name"
// +kubebuilder:printcolumn:name="RESOURCE-KIND",type="string",JSONPath=".resourceRef.kind"
// +kubebuilder:printcolumn:name="RESOURCE-NAME",type="string",JSONPath=".resourceRef.name"
// +kubebuilder:resource:scope=Cluster,categories={ndd,nddd},shortName=tu
type TargetUsage struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	nddv1.TargetUsage `json:",inline"`
}

// +kubebuilder:object:root=true

// TargetUsageList contains a list of TargetUsage
type TargetUsageList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []TargetUsage `json:"items"`
}

func init() {
	SchemeBuilder.Register(&Target{}, &TargetList{})
	SchemeBuilder.Register(&TargetUsage{}, &TargetUsageList{})
}
