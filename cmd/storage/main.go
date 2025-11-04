package main

import (
	proto "GOMinifyURL/internal/proto/storage"
	storageServer "GOMinifyURL/internal/storage/server"
	"GOMinifyURL/internal/storage/utils"
	"context"
	"fmt"
	"log"
	"log/slog"
	_ "log/slog"
	"net"
	"os"
	"path/filepath"
	"strings"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/spf13/pflag"
	"github.com/spf13/viper"
	"google.golang.org/grpc"
)

type Config struct {
	Node struct {
		Port     int    `mapstructure:"port"`
		Host     string `mapstructure:"host"`
		Hostname string `mapstructure:"hostname"`
		Id       string `mapstructure:"id"`
		Address  string `mapstructure:"addr"`
	}
	Database struct {
		Loc      string `mapstructure:"loc"`
		User     string `mapstructure:"user"`
		Password string `mapstructure:"password"`
		SSL      string `mapstructure:"ssl"`
		Table    string `mapstructure:"table"`
	}
	Cache struct {
		User     string `mapstructure:"user"`
		Loc      string `mapstructure:"loc"`
		Password string `mapstructure:"password"`
		SSL      string `mapstructure:"ssl"`
		Table    string `mapstructure:"table"`
	}
	Config  string `mapstructure:"config"`
	Verbose uint   `mapstructure:"verbose"`
	Mtls    struct {
		Enabled bool   `mapstructure:"enabled"`
		Cert    string `mapstructure:"cert"`
		Key     string `mapstructure:"key"`
		Ca      string `mapstructure:"ca"`
	}
	Dev struct {
		Environment bool `mapstructure:"environment"`
	}
	Oidc struct {
		Issuer struct {
			Url string `mapstructure:"url"`
		}
		Client struct {
			Id string `mapstructure:"id"`
		}
	}
}

func main() {

	viper.SetDefault("node.port", "8083")
	viper.SetDefault("node.host", "0.0.0.0")
	viper.SetDefault("node.hostname", "storage-server")
	viper.SetDefault("node.id", "storage-server")
	viper.SetDefault("node.address", fmt.Sprintf("%s:%d", "storage-server", 8083))

	// add extra case lookup for _ when using . and -
	viper.SetEnvKeyReplacer(strings.NewReplacer(".", "_", "-", "_"))
	pflag.StringP("node.host", "h", "", "Host to bind the admin grpc server to")
	pflag.IntP("node.port", "p", 0, "Port to bind the admin grpc server to")
	pflag.StringP("node.hostname", "hn", "", "Hostname of the node as reported to the admin server")
	pflag.StringP("node.id", "i", "", "ID of the node to which is sent to the admin server to uniquely ident this node")
	pflag.BoolP("mtls.enabled", "m", false, "Enable mtls mode")
	pflag.BoolP("dev.environment", "dev", false, "Enable development environment disables auth etc")
	// add middle ware logging or not
	pflag.UintP("verbose", "v", 0, "Verbose output")
	pflag.Parse()
	pflag.StringP("config", "c", "config.yaml", "location of the config file")
	pflag.Parse()

	flag := pflag.Lookup("config")
	if flag == nil {
		log.Fatal("config flag not found")
	}
	viper.SetConfigFile(filepath.Clean(flag.Value.String()))
	err := viper.ReadInConfig()
	if err != nil {
		log.Panic(err.Error())
		return
	}
	err = viper.BindPFlags(pflag.CommandLine)
	if err != nil {
		log.Panic(err.Error())
		return
	}

	viper.SetEnvPrefix("STORAGE")
	viper.AutomaticEnv()
	viper.MustBindEnv("node.host")
	viper.MustBindEnv("node.port")
	viper.MustBindEnv("node.hostname")
	viper.MustBindEnv("node.id")
	viper.MustBindEnv("node.address")
	viper.MustBindEnv("dev.environment")
	viper.MustBindEnv("verbose")
	viper.MustBindEnv("admin.loc")
	viper.MustBindEnv("database.loc")
	viper.MustBindEnv("database.user")
	viper.MustBindEnv("database.password")
	viper.MustBindEnv("database.ssl")
	viper.MustBindEnv("database.table")
	viper.MustBindEnv("cache.loc")
	viper.MustBindEnv("cache.user")
	viper.MustBindEnv("cache.password")
	viper.MustBindEnv("cache.ssl")
	viper.MustBindEnv("cache.table")
	viper.MustBindEnv("oidc.issuer.url")
	viper.MustBindEnv("oidc.client.id")
	viper.MustBindEnv("mtls.enabled")
	viper.MustBindEnv("mtls.cert")
	viper.MustBindEnv("mtls.key")
	viper.MustBindEnv("mtls.ca")

	pqDSN := utils.BuildPostgresDSN(viper.GetString("database.user"),
		viper.GetString("database.password"), viper.GetString("database.loc"),
		viper.GetString("database.table"), viper.GetString("database.ssl"))
	cxt, cancel := context.WithCancel(context.Background())
	defer cancel()
	pool, err := pgxpool.New(cxt, pqDSN)
	if err != nil {
		log.Panic(err.Error())
		return
	}
	defer pool.Close()
	if viper.GetUint("verbose") > 0 {
		logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
		slog.SetDefault(logger)
	}
	server := grpc.NewServer()
	proto.RegisterURLStorageServer(server, storageServer.NewStorageServer(pool))
	listener, err := net.Listen("tcp", fmt.Sprintf("%s:%d", viper.GetString("HOST"), viper.GetInt("PORT")))
	if err != nil {
		log.Panic(err.Error())
		return
	}
	_ = server.Serve(listener)
	return
}
