package tuya

import "time"

// Region configuration
type Region struct {
	Name        string `json:"name"`
	Host        string `json:"host"`
	Description string `json:"description"`
}

// Cookie for serialization
type Cookie struct {
	Name     string    `json:"name"`
	Value    string    `json:"value"`
	Domain   string    `json:"domain"`
	Path     string    `json:"path"`
	Expires  time.Time `json:"expires"`
	Secure   bool      `json:"secure"`
	HttpOnly bool      `json:"httpOnly"`
}

// SessionData contains all persistent authentication data
type SessionData struct {
	LoginResult   *LoginResult `json:"loginResult"`
	Cookies       []*Cookie    `json:"cookies"`
	LastValidated time.Time    `json:"lastValidated"`
	ServerHost    string       `json:"serverHost"`
	Region        string       `json:"region"`
	UserEmail     string       `json:"userEmail"`
}
