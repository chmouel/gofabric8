/**
 * Copyright (C) 2015 Red Hat, Inc.
 *
 * Licensed under the Apache License, Version 2.0 (the "License");
 * you may not use this file except in compliance with the License.
 * You may obtain a copy of the License at
 *
 *         http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing, software
 * distributed under the License is distributed on an "AS IS" BASIS,
 * WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
 * See the License for the specific language governing permissions and
 * limitations under the License.
 */
package cmds

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/pem"
	"fmt"
	"io/ioutil"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"golang.org/x/crypto/ssh"

	"github.com/fabric8io/gofabric8/client"
	"github.com/fabric8io/gofabric8/util"
	oclient "github.com/openshift/origin/pkg/client"
	tapi "github.com/openshift/origin/pkg/template/apis/template"
	tapiv1 "github.com/openshift/origin/pkg/template/apis/template/v1"
	"github.com/spf13/cobra"
	flag "github.com/spf13/pflag"
	"k8s.io/kubernetes/pkg/api"

	apim "k8s.io/apimachinery/pkg/apis/meta/v1"
	clientset "k8s.io/kubernetes/pkg/client/clientset_generated/internalclientset"
	cmdutil "k8s.io/kubernetes/pkg/kubectl/cmd/util"
	"k8s.io/kubernetes/pkg/labels"
	"k8s.io/kubernetes/pkg/runtime"
)

type Keypair struct {
	pub  []byte
	priv []byte
}

func NewCmdSecrets(f cmdutil.Factory) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "secrets",
		Short: "Set up Secrets on your Kubernetes or OpenShift environment",
		Long:  `set up Secrets on your Kubernetes or OpenShift environment`,
		PreRun: func(cmd *cobra.Command, args []string) {
			showBanner()
		},
		Run: func(cmd *cobra.Command, args []string) {
			c, cfg := client.NewClient(f)
			ns, _, _ := f.DefaultNamespace()
			util.Info("Setting up secrets on your ")
			util.Success(string(util.TypeOfMaster(c)))
			util.Info(" installation at ")
			util.Success(cfg.Host)
			util.Info(" in namespace ")
			util.Successf("%s\n\n", ns)

			yes := cmd.Flags().Lookup(yesFlag).Value.String() == "false"
			if confirmAction(yes) {
				tapi.AddToScheme(api.Scheme)
				tapiv1.AddToScheme(api.Scheme)
				count := 0

				typeOfMaster := util.TypeOfMaster(c)

				catalogSelector := map[string]string{
					"provider": "fabric8.io",
					"kind":     "catalog",
				}
				configmaps, err := c.ConfigMaps(ns).List(apim.ListOptions{
					LabelSelector: labels.Set(catalogSelector).AsSelector(),
				})
				if err != nil {
					fmt.Printf("Failed to load Catalog configmaps %s", err)
				} else {
					for _, configmap := range configmaps.Items {
						for key, data := range configmap.Data {
							obj, err := runtime.Decode(api.Codecs.UniversalDecoder(), []byte(data))
							if err != nil {
								util.Infof("Failed to decodeconfig map %s with key %s. Got error: %s", configmap.ObjectMeta.Name, key, err)
							} else {
								switch rc := obj.(type) {
								case *api.ReplicationController:
									for secretType, secretDataIdentifiers := range rc.Spec.Template.Annotations {
										count += createAndPrintSecrets(secretDataIdentifiers, secretType, c, f, cmd.Flags())
									}
								case *tapi.Template:
									count += processSecretsForTemplate(c, *rc, f, cmd)
								}
							}
						}
					}
				}

				if typeOfMaster != util.Kubernetes {
					oc, _ := client.NewOpenShiftClient(cfg)
					t := getTemplates(oc, ns)

					// get all the Templates and find the annotations on any Pods
					for _, i := range t.Items {
						count += processSecretsForTemplate(c, i, f, cmd)
					}
				}

				if count == 0 {
					util.Info("No secrets created as no fabric8 secrets annotations found in the Fabric8 Catalog\n")
					util.Info("For more details see: https://github.com/fabric8io/fabric8/blob/master/docs/secretAnnotations.md\n")
				}
			}
		},
	}
	cmd.PersistentFlags().BoolP("print-import-folder-structure", "", true, "Prints the folder structures that are being used by the template annotations to import secrets")
	cmd.PersistentFlags().BoolP("write-generated-keys", "", false, "Write generated secrets to the local filesystem")
	cmd.PersistentFlags().BoolP("generate-secrets-data", "g", true, "Generate secrets data if secrets cannot be found to import from the local filesystem")
	return cmd
}

func processSecretsForTemplate(c *clientset.Clientset, i tapi.Template, f cmdutil.Factory, cmd *cobra.Command) int {
	count := 0
	// convert TemplateList.Objects to Kubernetes resources
	errs := runtime.DecodeList(i.Objects, api.Codecs.UniversalDecoder())
	if len(errs) > 0 {
		fmt.Printf("Failed to decode templates %v", errs)
		os.Exit(2)
	}
	for _, rc := range i.Objects {
		switch rc := rc.(type) {
		case *api.ReplicationController:
			for secretType, secretDataIdentifiers := range rc.Spec.Template.Annotations {
				count += createAndPrintSecrets(secretDataIdentifiers, secretType, c, f, cmd.Flags())
			}
		}
	}
	return count
}

func getTemplates(c *oclient.Client, ns string) *tapi.TemplateList {
	templates, err := c.Templates(ns).List(apim.ListOptions{})
	if err != nil {
		util.Fatalf("No Templates found in namespace %s\n", ns)
	}
	return templates
}

func createSecret(c *clientset.Clientset, f cmdutil.Factory, flags *flag.FlagSet, secretDataIdentifiers string, secretType string, keysNames []string) (Result, error) {
	var secret = secret(secretDataIdentifiers, secretType, keysNames, flags)
	ns, _, err := f.DefaultNamespace()
	if err != nil {
		return Failure, err
	}
	rs, err := c.Secrets(ns).Create(&secret)
	if rs != nil {
		return Success, err
	}
	return Failure, err
}

func createAndPrintSecrets(secretDataIdentifiers string, secretType string, c *clientset.Clientset, fa cmdutil.Factory, flags *flag.FlagSet) int {
	count := 0
	// check to see if multiple public and private keys are needed
	var dataType = strings.Split(secretType, "/")
	switch dataType[1] {
	case "secret-ssh-key":
		items := strings.Split(secretDataIdentifiers, ",")
		for i := range items {
			var name = items[i]
			r, err := createSecret(c, fa, flags, name, secretType, nil)
			printResult(name+" secret", r, err)
			if err == nil {
				count++
			}
		}
	case "secret-ssh-public-key":
		// if this is just a public key then the secret name is at the start of the string
		f := func(c rune) bool {
			return c == ',' || c == '[' || c == ']'
		}
		secrets := strings.FieldsFunc(secretDataIdentifiers, f)
		numOfSecrets := len(secrets)

		var keysNames []string
		if numOfSecrets > 0 {
			// if multiple secrets
			for i := 1; i < numOfSecrets; i++ {
				keysNames = append(keysNames, secrets[i])
			}
		} else {
			// only single secret required
			keysNames[0] = "ssh-key.pub"
		}

		r, err := createSecret(c, fa, flags, secrets[0], secretType, keysNames)

		printResult(secrets[0]+" secret", r, err)
		if err == nil {
			count++
		}

	default:
		gpgKeyName := []string{"gpg.conf", "secring.gpg", "pubring.gpg", "trustdb.gpg"}
		r, err := createSecret(c, fa, flags, secretDataIdentifiers, secretType, gpgKeyName)
		printResult(secretDataIdentifiers+" secret", r, err)
		if err == nil {
			count++
		}
	}
	return count
}

func secret(name string, secretType string, keysNames []string, flags *flag.FlagSet) api.Secret {
	return api.Secret{
		ObjectMeta: api.ObjectMeta{
			Name: name,
		},
		Type: api.SecretType(secretType),
		Data: getSecretData(secretType, name, keysNames, flags),
	}
}

func check(e error) {
	if e != nil {
		util.Warnf("Warning: %s\n", e)
	}
}

func logSecretImport(file string) {
	util.Infof("Importing secret: %s\n", file)
}

func getSecretData(secretType string, name string, keysNames []string, flags *flag.FlagSet) map[string][]byte {
	var dataType = strings.Split(secretType, "/")
	var data = make(map[string][]byte)

	switch dataType[1] {
	case "secret-ssh-key":
		if flags.Lookup("print-import-folder-structure").Value.String() == "true" {
			logSecretImport(name + "/ssh-key")
			logSecretImport(name + "/ssh-key.pub")
		}

		sshKey, err1 := ioutil.ReadFile(name + "/ssh-key")
		sshKeyPub, err2 := ioutil.ReadFile(name + "/ssh-key.pub")

		// if we cant find the public and private key to import, and generation flag is set then lets generate the keys
		if (err1 != nil && err2 != nil) && flags.Lookup("generate-secrets-data").Value.String() == "true" {
			util.Info("No secrets found on local filesystem, generating SSH public and private key pair\n")
			keypair := generateSshKeyPair()
			if flags.Lookup("write-generated-keys").Value.String() == "true" {
				writeFile(name+"/ssh-key", keypair.priv)
				writeFile(name+"/ssh-key.pub", keypair.pub)
			}
			data["ssh-key"] = keypair.priv
			data["ssh-key.pub"] = keypair.pub

		} else if (err1 != nil || err2 != nil) && flags.Lookup("generate-secrets-data").Value.String() == "true" {
			util.Infof("Found some keys to import but with errors so unable to generate SSH public and private key pair. %s\n", name)
			check(err1)
			check(err2)
		} else {
			// if we're not generating the keys and there's an error importing them then still create the secret but with empty data
			check(err1)
			check(err2)

			data["ssh-key"] = sshKey
			data["ssh-key.pub"] = sshKeyPub
		}
		return data

	case "secret-ssh-public-key":

		for i := 0; i < len(keysNames); i++ {
			if flags.Lookup("print-import-folder-structure").Value.String() == "true" {
				logSecretImport(name + "/" + keysNames[i])
			}

			sshPub, err := ioutil.ReadFile(name + "/" + keysNames[i])
			// if we cant find the public key to import and generation flag is set then lets generate the key
			if (err != nil) && flags.Lookup("generate-secrets-data").Value.String() == "true" {
				util.Info("No secrets found on local filesystem, generating SSH public key\n")
				keypair := generateSshKeyPair()
				if flags.Lookup("write-generated-keys").Value.String() == "true" {
					writeFile(name+"/ssh-key.pub", keypair.pub)
				}
				data[keysNames[i]] = keypair.pub

			} else {
				// if we're not generating the keys and there's an error importing them then still create the secret but with empty data
				check(err)
				data[keysNames[i]] = sshPub
			}
		}
		return data

	case "secret-gpg-key":
		for i := 0; i < len(keysNames); i++ {
			if flags.Lookup("print-import-folder-structure").Value.String() == "true" {
				logSecretImport(name + "/" + keysNames[i])
			}
			gpg, err := ioutil.ReadFile(name + "/" + keysNames[i])
			check(err)

			data[keysNames[i]] = gpg
		}

	case "secret-hub-api-token":

		if flags.Lookup("print-import-folder-structure").Value.String() == "true" {
			logSecretImport(name + "/hub")
		}
		hub, err := ioutil.ReadFile(name + "/hub")
		check(err)

		data["hub"] = hub

	case "secret-ssh-config":

		if flags.Lookup("print-import-folder-structure").Value.String() == "true" {
			logSecretImport(name + "/config")
		}
		sshConfig, err := ioutil.ReadFile(name + "/config")
		check(err)

		data["config"] = sshConfig
	case "secret-docker-cfg":

		if flags.Lookup("print-import-folder-structure").Value.String() == "true" {
			logSecretImport(name + "/config.json")
		}
		dockerCfg, err := ioutil.ReadFile(name + "/config.json")
		check(err)

		data["config.json"] = dockerCfg

	case "secret-maven-settings":

		if flags.Lookup("print-import-folder-structure").Value.String() == "true" {
			logSecretImport(name + "/settings.xml")
		}
		mvn, err := ioutil.ReadFile(name + "/settings.xml")
		check(err)
		if err != nil && flags.Lookup("generate-secrets-data").Value.String() == "true" {
			defaultSettingsXML := "https://raw.githubusercontent.com/fabric8io/gofabric8/master/default-secrets/mvnsettings.xml"
			logSecretImport("Using deafult maven settings from " + defaultSettingsXML)
			resp, err := http.Get(defaultSettingsXML)
			if err != nil {
				util.Fatalf("Cannot get fabric8 version to deploy: %v", err)
			}
			defer resp.Body.Close()
			// read xml http response
			mvn, err := ioutil.ReadAll(resp.Body)
			if err != nil {
				util.Fatalf("Cannot get fabric8 version to deploy: %v", err)
			}
			data["settings.xml"] = mvn
		} else {
			data["settings.xml"] = mvn
		}

		return data
	default:
		util.Fatalf("No matching data type %s\n", dataType)
	}
	return data
}

func generateSshKeyPair() Keypair {

	priv, err := rsa.GenerateKey(rand.Reader, 2014)
	if err != nil {
		util.Fatalf("Error generating key: %s", err)
	}

	// Get der format. priv_der []byte
	priv_der := x509.MarshalPKCS1PrivateKey(priv)

	// pem.Block
	// blk pem.Block
	priv_blk := pem.Block{
		Type:    "RSA PRIVATE KEY",
		Headers: nil,
		Bytes:   priv_der,
	}

	// Resultant private key in PEM format.
	// priv_pem string
	priv_pem := string(pem.EncodeToMemory(&priv_blk))

	// Public Key generation
	sshPublicKey, err := ssh.NewPublicKey(&priv.PublicKey)
	pubBytes := ssh.MarshalAuthorizedKey(sshPublicKey)

	return Keypair{
		pub:  []byte(pubBytes),
		priv: []byte(priv_pem),
	}
}

func writeFile(path string, contents []byte) {
	dir := strings.Split(path, string(filepath.Separator))
	os.MkdirAll("."+string(filepath.Separator)+dir[0], 0700)
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_TRUNC|os.O_CREATE, 0600)
	if err != nil {
		panic(err)
	}
	defer f.Close()
	if _, err := f.Write(contents); err != nil {
		panic(err)
	}
}
