/*
Copyright 2021 KubeCube Authors

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

package ldap

import (
	"fmt"
	"github.com/kubecube-io/kubecube/pkg/authentication"
	"github.com/kubecube-io/kubecube/pkg/authentication/identityprovider"
	"k8s.io/apimachinery/pkg/api/errors"
	"net/http"

	"github.com/go-ldap/ldap"
	"github.com/kubecube-io/kubecube/pkg/clog"
)

const (
	ldapAttributeObjectClass    = "objectClass"
	ldapAttributeObjectCategory = "objectCategory"
)

var Config = authentication.LdapConfig{}

func IsLdapOpen() bool {
	return Config.LdapIsEnable
}

type ldapProvider struct {
	LdapObjectClass      string `yaml:"ldapObjectClass,omitempty"`
	LdapLoginNameConfig  string `yaml:"ldapLoginNameConfig,omitempty"`
	LdapObjectCategory   string `yaml:"ldapObjectCategory, omitempty"`
	LdapServer           string `yaml:"ldapServer, omitempty"`
	LdapPort             string `yaml:"ldapPort, omitempty"`
	LdapBaseDN           string `yaml:"ldapBaseDN, omitempty"`
	LdapAdminUserAccount string `yaml:"ldapAdminUserAccount, omitempty"`
	LdapAdminPassword    string `yaml:"ldapAdminPassword, omitempty"`
}

type ldapIdentity struct {
	Username string
}

func (l *ldapIdentity) GetRespHeader() http.Header {
	return nil
}

func (l *ldapIdentity) GetUserName() string {
	return l.Username
}

func (l *ldapIdentity) GetGroup() string {
	return ""
}

func (l *ldapIdentity) GetUserEmail() string {
	return ""
}

func GetProvider() ldapProvider {
	return ldapProvider{
		LdapObjectClass:      Config.LdapObjectClass,
		LdapLoginNameConfig:  Config.LdapLoginNameConfig,
		LdapObjectCategory:   Config.LdapObjectCategory,
		LdapServer:           Config.LdapServer,
		LdapPort:             Config.LdapPort,
		LdapBaseDN:           Config.LdapBaseDN,
		LdapAdminUserAccount: Config.LdapAdminUserAccount,
		LdapAdminPassword:    Config.LdapAdminPassword}
}

func (l *ldapIdentity) GetUserID() string {
	return l.Username
}

func (l *ldapIdentity) GetUsername() string {
	return l.Username
}

func (l ldapProvider) Authenticate(username string, password string) (identityprovider.Identity, error) {

	// create connection by admin account and password
	conn, err := l.newConn()
	if err != nil {
		clog.Error("%s", err)
		return nil, err
	}

	// request to ldap server with user name
	filter := "("
	if l.LdapObjectCategory != "" || l.LdapObjectClass != "" {
		filter += "&"
	}
	if l.LdapObjectCategory != "" {
		filter = fmt.Sprintf("(%s=%s)", ldapAttributeObjectCategory, l.LdapObjectCategory)
	}
	if l.LdapObjectClass != "" {
		filter += fmt.Sprintf("(%s=%s)", ldapAttributeObjectClass, l.LdapObjectClass)
	}
	filter += fmt.Sprintf("(%s=%s))", l.LdapLoginNameConfig, username)
	result, err := conn.Search(&ldap.SearchRequest{
		BaseDN:       l.LdapBaseDN,
		Scope:        ldap.ScopeWholeSubtree,
		DerefAliases: ldap.NeverDerefAliases,
		SizeLimit:    1,
		TimeLimit:    0,
		TypesOnly:    false,
		Filter:       filter,
	})
	if err != nil {
		clog.Error("search ldap err: %s", err)
		return nil, err
	}

	// if response result num != 1, password is wrong
	if result == nil || len(result.Entries) != 1 {
		clog.Debug("result is null or result is not only")
		return nil, errors.NewUnauthorized("incorrect password")
	}

	// request to ldap server with result username and user password input
	entry := result.Entries[0]
	if err = conn.Bind(entry.DN, password); err != nil {
		clog.Info("verify user password")
		if ldap.IsErrorWithCode(err, ldap.LDAPResultInvalidCredentials) {
			clog.Info("password wrong")
			return nil, errors.NewUnauthorized("incorrect password")
		}
		return nil, err
	}

	defer conn.Close()

	return &ldapIdentity{
		Username: username,
	}, nil
}

func (l *ldapProvider) newConn() (*ldap.Conn, error) {
	var host = l.LdapServer
	if l.LdapPort != "" {
		host += ":" + l.LdapPort
	}
	conn, err := ldap.Dial("tcp", host)
	if err != nil {
		clog.Error("connect to ldap server err: %s", err)
		return nil, err
	}

	err = conn.Bind(l.LdapAdminUserAccount, l.LdapAdminPassword)
	if err != nil {
		clog.Error("bind ldap server by admin password error: %s", err)
		return nil, err
	}
	return conn, nil
}