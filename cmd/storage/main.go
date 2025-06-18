package storage

import (
	_ "GOMinifyURL/internal/storage/server"
	"GOMinifyURL/internal/storage/utils"
	"context"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/spf13/pflag"
	"github.com/spf13/viper"
	"log"
	"path/filepath"
)

func main() {
	pflag.String("config", "config.yaml", "location of the config file")
	pflag.Parse()
	err := viper.BindPFlags(pflag.CommandLine)
	if err != nil {
		log.Panic(err.Error())
		return
	}
	viper.SetConfigFile(filepath.Clean(viper.GetString("config")))
	viper.BindEnv("ID") // node id
	viper.BindEnv("ADMIN_LOCATION")
	viper.BindEnv("PQL_LOCATION")
	viper.BindEnv("PQL_PASSWORD")
	viper.BindEnv("PQL_USERNAME")
	viper.BindEnv("PQL_SSL_MODE")
	viper.BindEnv("PQL_DB")
	viper.BindEnv("REDIS_LOCATION")
	viper.BindEnv("REDIS_PASSWORD")
	viper.BindEnv("REDIS_USERNAME")
	viper.SetDefault("HOST", "localhost")
	viper.SetDefault("PORT", "8080")
	err = viper.ReadInConfig()
	if err != nil {
		log.Panic(err.Error())
		return
	}
	viper.AutomaticEnv()

	pqDSN := utils.BuildPostgresDSN(viper.GetString("PQL_USERNAME"),
		viper.GetString("PQL_PASSWORD"), viper.GetString("PQL_LOCATION"),
		viper.GetString("PQL_DB"), viper.GetString("PQL_SSL_MODE"))
	ctx := context.Background()

	pool, err := pgxpool.New(ctx, pqDSN)
	if err != nil {
		log.Panic(err.Error())
		return
	}
	defer pool.Close()

}
