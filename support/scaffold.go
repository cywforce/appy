package support

import (
	"encoding/hex"
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"text/template"

	"github.com/pkg/errors"
)

// ScaffoldOptions contains the information of how a new application should be
// created.
type ScaffoldOptions struct {
	// DBAdapter indicates the database adapter to use. By default, it is
	// "postgres". Possible values are "mysql" and "postgres".
	DBAdapter string

	// Description indicates the project description that will be used in HTML's
	// description meta tag, package.json and CLI help.
	Description string
}

// Scaffold creates a new application.
func Scaffold(options ScaffoldOptions) error {
	if options.DBAdapter == "" {
		options.DBAdapter = "postgres"
	}

	if !ArrayContains(SupportedDBAdapters, options.DBAdapter) {
		return errors.Errorf("DBAdapter '%s' is not supported, only '%s' are supported", options.DBAdapter, SupportedDBAdapters)
	}

	moduleName := ModuleName()
	_, dirname, _, _ := runtime.Caller(0)
	tplPath := filepath.Dir(dirname) + "/templates/scaffold"

	masterKeyDev := hex.EncodeToString(GenerateRandomBytes(32))
	masterKeyTest := hex.EncodeToString(GenerateRandomBytes(32))

	var dbURIPrimaryDev, dbURIPrimaryTest string
	switch options.DBAdapter {
	case "mysql":
		dbURIPrimaryDev = getEncryptedValue(fmt.Sprintf("mysql://root:whatever@0.0.0.0:23306/%s", moduleName), masterKeyDev)
		dbURIPrimaryTest = getEncryptedValue(fmt.Sprintf("mysql://root:whatever@0.0.0.0:23306/%s_test", moduleName), masterKeyTest)
	case "postgres":
		dbURIPrimaryDev = getEncryptedValue(fmt.Sprintf("postgresql://postgres:whatever@0.0.0.0:25432/%s?sslmode=disable&connect_timeout=5", moduleName), masterKeyDev)
		dbURIPrimaryTest = getEncryptedValue(fmt.Sprintf("postgresql://postgres:whatever@0.0.0.0:25432/%s_test?sslmode=disable&connect_timeout=5", moduleName), masterKeyTest)
	}

	httpCSRFSecretDev := getEncryptedValue(hex.EncodeToString(GenerateRandomBytes(32)), masterKeyDev)
	httpSessionSecretsDev := getEncryptedValue(hex.EncodeToString(GenerateRandomBytes(32)), masterKeyDev)
	workerRedisAddrDev := getEncryptedValue("0.0.0.0:26379", masterKeyDev)

	httpCSRFSecretTest := getEncryptedValue(hex.EncodeToString(GenerateRandomBytes(32)), masterKeyTest)
	httpSessionSecretsTest := getEncryptedValue(hex.EncodeToString(GenerateRandomBytes(32)), masterKeyTest)
	workerRedisAddrTest := getEncryptedValue("0.0.0.0:26379", masterKeyTest)

	if err := filepath.Walk(tplPath,
		func(src string, info os.FileInfo, err error) error {
			if err != nil {
				return err
			}

			dest := strings.ReplaceAll(src, tplPath+"/", "")
			dest = strings.TrimSuffix(dest, ".tpl")
			if info.IsDir() {
				if err := os.MkdirAll(dest, 0777); err != nil {
					return err
				}

				return nil
			}

			buf, err := ioutil.ReadFile(src)
			if err != nil {
				return err
			}

			file, err := os.Create(dest)
			if err != nil {
				return err
			}

			tpl, err := template.New("scaffold").Parse(strings.ReplaceAll(string(buf), "// Code generated by github.com/appist/appy.\n\n", ""))
			if err != nil {
				return err
			}

			return tpl.Execute(file, map[string]string{
				"assetWelcomeCSS":         "{{assetPath(`styles/welcome.css`)}}",
				"blockHead":               "{{block head()}}",
				"blockBody":               "{{block body()}}",
				"blockEnd":                "{{end}}",
				"dbAdapter":               options.DBAdapter,
				"dbURIPrimaryDev":         dbURIPrimaryDev,
				"httpCSRFSecretDev":       httpCSRFSecretDev,
				"httpSessionSecretsDev":   httpSessionSecretsDev,
				"workerRedisAddrDev":      workerRedisAddrDev,
				"dbURIPrimaryTest":        dbURIPrimaryTest,
				"httpCSRFSecretTest":      httpCSRFSecretTest,
				"httpSessionSecretsTest":  httpSessionSecretsTest,
				"workerRedisAddrTest":     workerRedisAddrTest,
				"extendApplicationLayout": "{{extends \"../layouts/application.html\"}}",
				"projectName":             moduleName,
				"projectDesc":             options.Description,
				"masterKeyDev":            masterKeyDev,
				"masterKeyTest":           masterKeyTest,
				"translateWelcome":        "{{t(\"welcome\", `{\"Name\": \"John Doe\", \"Title\": \"` + t(\"title\") + `\"}`)}}",
				"yieldHead":               "{{yield head()}}",
				"yieldBody":               "{{yield body()}}",
			})
		}); err != nil {
		return err
	}

	version := strings.ReplaceAll(runtime.Version(), "go", "")
	versionSplits := strings.Split(version, ".")

	return ioutil.WriteFile(
		"go.mod",
		[]byte(`module `+moduleName+`

go `+versionSplits[0]+"."+versionSplits[1]+`

require (
	github.com/99designs/gqlgen v0.11.3
	github.com/appist/appy latest
	github.com/vektah/gqlparser/v2 v2.0.1
)`),
		0777,
	)
}

func getEncryptedValue(value string, masterKey string) string {
	plaintext := []byte(value)
	ciphertext, _ := AESEncrypt(plaintext, []byte(masterKey))

	return hex.EncodeToString(ciphertext)
}
