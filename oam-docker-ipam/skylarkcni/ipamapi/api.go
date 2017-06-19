// Package api defines the data structure to be used in the request/response
// messages between libnetwork and the remote ipam plugin
package ipamapi

import (
	"net/http"
	"github.com/docker/go-plugins-helpers/sdk"
)

const (
	manifest = `{"Implements": ["IpamDriver"]}`

	capabilitiesPath   = "/IpamDriver.GetCapabilities"
	addressSpacesPath  = "/IpamDriver.GetDefaultAddressSpaces"
	requestPoolPath    = "/IpamDriver.RequestPool"
	releasePoolPath    = "/IpamDriver.ReleasePool"
	requestAddressPath = "/IpamDriver.RequestAddress"
	releaseAddressPath = "/IpamDriver.ReleaseAddress"
	getAddressPath     = "/IpamDriver.GetAddress"
)

// PluginPath is the path to the listen socket directory for skylark
const PluginPath = "/run/docker/plugins"

// IpamSocket is the full path to the listen socket for skylark
const IpamSocket = "/run/docker/plugins/skylark.sock"

// Ipam represent the interface a driver must fulfill.
type Ipam interface {
	GetCapabilities() (*CapabilitiesResponse, error)
	GetDefaultAddressSpaces() (*AddressSpacesResponse, error)
	RequestPool(*RequestPoolRequest) (*RequestPoolResponse, error)
	ReleasePool(*ReleasePoolRequest) error
	RequestAddress(*RequestAddressRequest) (*RequestAddressResponse, error)
	ReleaseAddress(*ReleaseAddressRequest) error
	GetAddress(*GetAddressRequest) (*GetAddressResponse, error)
}

// Response is the basic response structure used in all responses
type Response struct {
	Error string
}

// IsSuccess returns wheter the plugin response is successful
func (r *Response) IsSuccess() bool {
	return r.Error == ""
}

// GetError returns the error from the response, if any.
func (r *Response) GetError() string {
	return r.Error
}

// CapabilitiesResponse is the response of GetCapability request
type CapabilitiesResponse struct {
	//Response
	RequiresMACAddress bool
}

// ToCapability converts the capability response into the internal ipam driver capaility structure
//func (capRes GetCapabilityResponse) ToCapability() *ipamapi.Capability {
//	return &ipamapi.Capability{RequiresMACAddress: capRes.RequiresMACAddress}
//}

// AddressSpacesResponse is the response to the ``get default address spaces`` request message
type AddressSpacesResponse struct {
	//Response
	LocalDefaultAddressSpace  string
	GlobalDefaultAddressSpace string
}

// RequestPoolRequest represents the expected data in a ``request address pool`` request message
type RequestPoolRequest struct {
	AddressSpace string
	Pool         string
	SubPool      string
	Options      map[string]string
	V6           bool
}

// RequestPoolResponse represents the response message to a ``request address pool`` request
type RequestPoolResponse struct {
	//Response
	PoolID string
	Pool   string // CIDR format
	Data   map[string]string
}

// ReleasePoolRequest represents the expected data in a ``release address pool`` request message
type ReleasePoolRequest struct {
	PoolID string
}

// ReleasePoolResponse represents the response message to a ``release address pool`` request
type ReleasePoolResponse struct {
	//Response
}

// RequestAddressRequest represents the expected data in a ``request address`` request message
type RequestAddressRequest struct {
	PoolID  string
	Address string
	Options map[string]string
}

// RequestAddressResponse represents the expected data in the response message to a ``request address`` request
type RequestAddressResponse struct {
	//Response
	Address string // in CIDR format
	Data    map[string]string
}

// ReleaseAddressRequest represents the expected data in a ``release address`` request message
type ReleaseAddressRequest struct {
	PoolID  string
	Address string
}

// ReleaseAddressResponse represents the response message to a ``release address`` request
type ReleaseAddressResponse struct {
	//Response
}

// GetAddressRequest get the ip address by container id (only used for cni)
type GetAddressRequest struct {
	ContainerID string
}

// GetAddressResponse returns ip address by specified container id
type GetAddressResponse struct {
	Address string
}


// ErrorResponse is a formatted error message that libnetwork can understand
type ErrorResponse struct {
	Err string
}

// NewErrorResponse creates an ErrorResponse with the provided message
func NewErrorResponse(msg string) *ErrorResponse {
	return &ErrorResponse{Err: msg}
}

// Handler forwards requests and responses between the docker daemon and the plugin.
type Handler struct {
	ipam Ipam
	sdk.Handler
}

// NewHandler initializes the request handler with a driver implementation.
func NewHandler(ipam Ipam) *Handler {
	h := &Handler{ipam, sdk.NewHandler(manifest)}
	h.initMux()
	return h
}

func (h *Handler) initMux() {
	h.HandleFunc(capabilitiesPath, func(w http.ResponseWriter, r *http.Request) {
		res, err := h.ipam.GetCapabilities()
		if err != nil {
			msg := err.Error()
			sdk.EncodeResponse(w, NewErrorResponse(msg), msg)
			return
		}
		sdk.EncodeResponse(w, res, "")
	})
	h.HandleFunc(addressSpacesPath, func(w http.ResponseWriter, r *http.Request) {
		res, err := h.ipam.GetDefaultAddressSpaces()
		if err != nil {
			msg := err.Error()
			sdk.EncodeResponse(w, NewErrorResponse(msg), msg)
			return
		}
		sdk.EncodeResponse(w, res, "")
	})
	h.HandleFunc(requestPoolPath, func(w http.ResponseWriter, r *http.Request) {
		req := &RequestPoolRequest{}
		err := sdk.DecodeRequest(w, r, req)
		if err != nil {
			return
		}
		res, err := h.ipam.RequestPool(req)
		if err != nil {
			msg := err.Error()
			sdk.EncodeResponse(w, NewErrorResponse(msg), msg)
			return
		}
		sdk.EncodeResponse(w, res, "")
	})
	h.HandleFunc(releasePoolPath, func(w http.ResponseWriter, r *http.Request) {
		req := &ReleasePoolRequest{}
		err := sdk.DecodeRequest(w, r, req)
		if err != nil {
			return
		}
		err = h.ipam.ReleasePool(req)
		if err != nil {
			msg := err.Error()
			sdk.EncodeResponse(w, NewErrorResponse(msg), msg)
			return
		}
		sdk.EncodeResponse(w, make(map[string]string), "")
	})
	h.HandleFunc(requestAddressPath, func(w http.ResponseWriter, r *http.Request) {
		req := &RequestAddressRequest{}
		err := sdk.DecodeRequest(w, r, req)
		if err != nil {
			return
		}
		res, err := h.ipam.RequestAddress(req)
		if err != nil {
			msg := err.Error()
			sdk.EncodeResponse(w, NewErrorResponse(msg), msg)
			return
		}
		sdk.EncodeResponse(w, res, "")
	})
	h.HandleFunc(releaseAddressPath, func(w http.ResponseWriter, r *http.Request) {
		req := &ReleaseAddressRequest{}
		err := sdk.DecodeRequest(w, r, req)
		if err != nil {
			return
		}
		err = h.ipam.ReleaseAddress(req)
		if err != nil {
			msg := err.Error()
			sdk.EncodeResponse(w, NewErrorResponse(msg), msg)
		}
		sdk.EncodeResponse(w, make(map[string]string), "")
	})
	h.HandleFunc(getAddressPath, func(w http.ResponseWriter, r *http.Request) {
		req := &GetAddressRequest{}
		err := sdk.DecodeRequest(w, r, req)
		if err != nil {
			return
		}
		res, err := h.ipam.GetAddress(req)
		if err != nil {
			msg := err.Error()
			sdk.EncodeResponse(w, NewErrorResponse(msg), msg)
			return
		}
		sdk.EncodeResponse(w, res, "")
	})

}
