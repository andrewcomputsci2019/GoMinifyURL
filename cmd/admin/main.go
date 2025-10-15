package admin

import (
	"log"

	"github.com/spf13/pflag"
	"github.com/spf13/viper"
)

func main() {
	//program init code todo
	log.SetPrefix("[admin]: ")
	log.SetFlags(log.Ldate | log.Ltime | log.Lshortfile)

	pflag.StringP("host", "h", "", "Host to bind the admin server to")
	pflag.IntP("port", "p", 8080, "Port to bind the admin server to")
	pflag.UintP("leaseDuration", "l", 0, "Lease duration in seconds")
	pflag.UintP("verbose", "v", 0, "Verbose output")
	pflag.Parse()

	err := viper.BindPFlags(pflag.CommandLine)
	if err != nil {
		log.Fatal(err)
		return
	}
	viper.MustBindEnv("OIDC_ISSUER_URL")
	viper.MustBindEnv("OIDC_CLIENT_ID")

	viper.AutomaticEnv()

	// todo start grpc server and http admin server
}
