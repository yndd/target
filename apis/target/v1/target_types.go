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
)

// TargetSpec struct
type TargetSpec struct {
	// Properties define the properties of the Target
	Properties *TargetProperties `json:"properties,omitempty"`
	Status     *TargetStatus     `json:"status,omitempty"`
}

// TargetStatus defines the observed state of TargetNode
type TargetStatus struct {
	// identifies the status of the reconciliation process
	nddv1.ConditionedStatus `json:",inline"`
	// identifies the controller reference of the target
	ControllerRef nddv1.Reference `json:"controllerRef,omitempty"`
	// identifies the operational information of the target
	DiscoveryInfo *DiscoveryInfo `json:"discoveryInfo,omitempty"`
}

// TargetProperties define the properties of the Target
type TargetProperties struct {
	//+kubebuilder:validation:Enum=unknown;nokiaSRL;nokiaSROS;
	VendorType VendorType             `json:"vendorType,omitempty"`
	Config     *TargetConfig          `json:"config,omitempty"`
	Allocation map[string]*Allocation `json:"allocation,omitempty"`
}

type TargetConfig struct {
	Address        string `json:"address,omitempty"`
	CredentialName string `json:"credentialName,omitempty"`
	//+kubebuilder:validation:Enum=unknown;JSON;JSON_IETF;bytes;protobuf;ASCII;
	Encoding Encoding `json:"encoding,omitempty"`
	Insecure bool     `json:"insecure,omitempty"`
	//+kubebuilder:validation:Enum=unknown;gnmi;netconf;
	Protocol          Protocol `json:"protocol,omitempty"`
	Proxy             string   `json:"proxy,omitempty"`
	SkipVerify        bool     `json:"skipVerify,omitempty"`
	TlsCredentialName string   `json:"tlsCredentialName,omitempty"`
}

type Encoding string

const (
	Encoding_Unknown   Encoding = "unknown"
	Encoding_JSON      Encoding = "JSON"
	Encoding_JSON_IETF Encoding = "JSON_IETF"
	Encoding_Bytes     Encoding = "bytes"
	Encoding_Protobuf  Encoding = "protobuf"
	Encoding_Ascii     Encoding = "ASCII"
)

type Protocol string

const (
	Protocol_Unknown Encoding = "unknown"
	Protocol_GNMI    Encoding = "gnmi"
	Protocol_NETCONF Encoding = "netconf"
)

type Allocation struct {
	ServiceIdentity string `json:"serviceIdentity,omitempty"`
	ServiceName     string `json:"serviceName,omitempty"`
}

// DiscoveryInfo collects information about the target
type DiscoveryInfo struct {
	// the VendorType of the target
	VendorType VendorType `json:"vendorType,omitempty"`

	// Host name of the target
	HostName string `json:"hostname,omitempty"`

	// the Kind of the target
	Platform string `json:"platform,omitempty"`

	// SW version of the target
	SwVersion string `json:"swVersion,omitempty"`

	// the Mac address of the target
	MacAddress string `json:"macAddress,omitempty"`

	// the Serial Number of the target
	SerialNumber string `json:"serialNumber,omitempty"`

	// Supported Encodings of the target
	SupportedEncodings []string `json:"supportedEncodings,omitempty"`

	// Last discovery time in nanoseconds
	LastSeen int64 `json:"lastSeen,omitempty"`
}

// // Status defines the observed state of the Target
// type Status struct {
// 	// The DiscoveryInfo of the Target
// 	DiscoveryInfo *DiscoveryInfo `json:"discoveryInfo,omitempty"`

// 	// UsedTargetSpec identifies the used target spec when installed
// 	// UsedTargetSpec *TargetSpec `json:"usedTargetSpec,omitempty"`
// }

// +kubebuilder:object:root=true
// +genclient
// +genclient:nonNamespaced

// Target is the Schema for the targets API
// +kubebuilder:subresource:status
// +kubebuilder:printcolumn:name="SYNC",type="string",JSONPath=".status.conditions[?(@.kind=='Synced')].status"
// +kubebuilder:printcolumn:name="READY",type="string",JSONPath=".status.conditions[?(@.kind=='Ready')].status"
// +kubebuilder:printcolumn:name="ADDRESS",type="string",JSONPath=".spec.properties.config.address",description="address to connect to the target'"
// +kubebuilder:printcolumn:name="PROTOCOL",type="string",JSONPath=".spec.properties.config.protocol",description="Protocol used to communicate to the target"
// +kubebuilder:printcolumn:name="VENDORTYPE",type="string",JSONPath=".status.discoveryInfo.vendorType",description="VendorType of target"
// +kubebuilder:printcolumn:name="PLATFORM",type="string",JSONPath=".status.discoveryInfo.platform",description="Platform of target"
// +kubebuilder:printcolumn:name="SWVERSION",type="string",JSONPath=".status.discoveryInfo.swVersion",description="SW version of the target"
// +kubebuilder:printcolumn:name="MACADDRESS",type="string",JSONPath=".status.discoveryInfo.macAddress",description="macAddress of the target"
// +kubebuilder:printcolumn:name="SERIALNBR",type="string",JSONPath=".status.discoveryInfo.serialNumber",description="serialNumber of the target"
// +kubebuilder:printcolumn:name="LASTSEEN",type="string",JSONPath=".status.discoveryInfo.lastSeen",description="serialNumber of the target"
// +kubebuilder:printcolumn:name="AGE",type="date",JSONPath=".metadata.creationTimestamp"
// +kubebuilder:resource:categories={ndd,nddd}, shortName=t
type Target struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec TargetSpec `json:"spec,omitempty"`
}

//+kubebuilder:object:root=true

// TargetList contains a list of targets
type TargetList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []Target `json:"items"`
}

func init() {
	SchemeBuilder.Register(&Target{}, &TargetList{})
}
