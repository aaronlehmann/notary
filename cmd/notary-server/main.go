package main

import (
	"crypto/tls"
	"crypto/x509"
	"database/sql"
	_ "expvar"
	"flag"
	"fmt"
	"io/ioutil"
	"net/http"
	_ "net/http/pprof"
	"os"
	"os/signal"
	"path/filepath"
	"regexp"
	"strings"
	"syscall"

	"github.com/Sirupsen/logrus"
	_ "github.com/docker/distribution/registry/auth/token"
	"github.com/endophage/gotuf/signed"
	"github.com/go-sql-driver/mysql"
	"golang.org/x/net/context"

	"github.com/docker/notary/server"
	"github.com/docker/notary/server/storage"
	"github.com/docker/notary/signer"
	"github.com/spf13/viper"
)

// DebugAddress is the debug server address to listen on
const DebugAddress = "localhost:8080"

var debug bool
var configFile string

func init() {
	// set default log level to Error
	viper.SetDefault("logging.level", 2)

	// Setup flags
	flag.StringVar(&configFile, "config", "", "Path to configuration file")
	flag.BoolVar(&debug, "debug", false, "Enable the debugging server on localhost:8080")
}

func main() {
	flag.Usage = usage
	flag.Parse()

	if debug {
		go debugServer(DebugAddress)
	}

	ctx := context.Background()

	filename := filepath.Base(configFile)
	ext := filepath.Ext(configFile)
	configPath := filepath.Dir(configFile)

	viper.SetConfigType(strings.TrimPrefix(ext, "."))
	viper.SetConfigName(strings.TrimSuffix(filename, ext))
	viper.AddConfigPath(configPath)
	err := viper.ReadInConfig()
	if err != nil {
		logrus.Error("Viper Error: ", err.Error())
		logrus.Error("Could not read config at ", configFile)
		os.Exit(1)
	}
	logrus.SetLevel(logrus.Level(viper.GetInt("logging.level")))

	sigHup := make(chan os.Signal)
	sigTerm := make(chan os.Signal)

	signal.Notify(sigHup, syscall.SIGHUP)
	signal.Notify(sigTerm, syscall.SIGTERM)

	var trust signed.CryptoService
	if viper.GetString("trust_service.type") == "remote" {
		logrus.Info("[Notary Server] : Using remote signing service")
		trust = signer.NewRufusSigner(
			viper.GetString("trust_service.hostname"),
			viper.GetString("trust_service.port"),
			viper.GetString("trust_service.tls_ca_file"),
		)
	} else {
		logrus.Info("[Notary Server] : Using local signing service")
		trust = signed.NewEd25519()
	}

	if viper.GetString("store.backend") == "mysql" {
		ctx = context.WithValue(ctx, "metaStore", storage.NewMySQLStorage(getMysqlDB()))
	} else {
		ctx = context.WithValue(ctx, "metaStore", storage.NewMemStorage())
	}
	logrus.Info("[Notary Server] Starting Server")
	err = server.Run(
		ctx,
		viper.GetString("server.addr"),
		viper.GetString("server.tls_cert_file"),
		viper.GetString("server.tls_key_file"),
		trust,
	)

	logrus.Error("[Notary Server]", err.Error())
	return
}

func usage() {
	fmt.Println("usage:", os.Args[0])
	flag.PrintDefaults()
}

// debugServer starts the debug server with pprof, expvar among other
// endpoints. The addr should not be exposed externally. For most of these to
// work, tls cannot be enabled on the endpoint, so it is generally separate.
func debugServer(addr string) {
	logrus.Info("[Notary Debug Server] server listening on", addr)
	if err := http.ListenAndServe(addr, nil); err != nil {
		logrus.Fatal("[Notary Debug Server] error listening on debug interface: ", err)
	}
}

func getMysqlDB() *sql.DB {
	dbURL := viper.GetString("storage.db_url")
	useSSL := viper.GetBool("storage.db_use_ssl")
	if useSSL {
		tlsConfig := &tls.Config{}
		caCert := viper.GetString("storage.db_ssl_ca_cert")
		if caCert != "" {
			rootCertPool := x509.NewCertPool()
			pem, err := ioutil.ReadFile(caCert)
			if err != nil {
				logrus.Fatal("[Notary Server] Error loading DB CA certificate:", err)
			}
			if ok := rootCertPool.AppendCertsFromPEM(pem); !ok {
				logrus.Fatal("[Notary Server] Error appending DB CA certificates from PEM file:", err)
			}
			tlsConfig.RootCAs = rootCertPool
		}
		mysql.RegisterTLSConfig("notary", tlsConfig)

		// Append a "tls=notary" parameter to the DSN. Unfortunately,
		// this takes some care to handle existing DSNs with and without
		// parameters.
		if regexp.MustCompile("/.*\\?").MatchString(dbURL) {
			dbURL += "&tls=notary"
		} else {
			dbURL += "?tls=notary"
		}
	}
	db, err := sql.Open("mysql", dbURL)
	if err != nil {
		logrus.Fatal("[Notary Server] Error starting DB driver: ", err.Error())
	}

	return db
}
