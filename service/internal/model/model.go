// Package model holds the dependency-free data types passed between the broker
// and its IdP / Kubernetes adapters.
package model

// Identity is the verified subject returned by an IdP, including group memberships.
type Identity struct {
	Subject       string
	Email         string
	EmailVerified bool
	Domain        string // provider-resolved: "hd" for Google, email domain otherwise
	Name          string
	Groups        []Membership
}

// Membership is a group the user belongs to. Gid is the value matched against
// the rbac.kargus.io/group annotation by the operator.
type Membership struct {
	Gid    string
	Name   string
	Domain string
}

// Token is a minted, short-lived ServiceAccount token.
type Token struct {
	AccessToken string
	ExpiresIn   int // seconds
}
