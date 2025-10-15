package main

import (
	"GOMinifyURL/internal/admin"
	"GOMinifyURL/internal/middleware/logger"
	"context"
	"fmt"
	"log"
	"os/signal"
	"syscall"
	"time"

	"github.com/spf13/pflag"
	"github.com/spf13/viper"
)

const (
	ColorReset  = "\033[0m"
	ColorRed    = "\033[0;31m"
	ColorGreen  = "\033[0;32m"
	ColorYellow = "\033[0;33m"
	ColorBlue   = "\033[0;34m"
	ColorPurple = "\033[0;35m"
	ColorCyan   = "\033[0;36m"
)

func main() {
	log.SetPrefix("[admin]: ")

	pflag.StringP("grpc_host", "gh", "", "Host to bind the admin grpc server to")
	pflag.IntP("grpc_port", "gp", 8083, "Port to bind the admin grpc server to")
	pflag.StringP("http_host", "h", "", "Host to bind the admin http server to")
	pflag.IntP("http_port", "p", 8080, "Port to bind the admin http server to")
	pflag.UintP("leaseDuration", "l", 0, "Lease duration in seconds, min value 15")
	// add middle ware logging or not
	pflag.UintP("verbose", "v", 0, "Verbose output")
	pflag.Parse()

	err := viper.BindPFlags(pflag.CommandLine)
	if err != nil {
		log.Fatal(err)
		return
	}
	// needed down the line for oauth signing etc
	viper.MustBindEnv("OIDC_ISSUER_URL")
	viper.MustBindEnv("OIDC_CLIENT_ID")
	grpcAddr := fmt.Sprintf("%s:%d", viper.GetString("grpc_host"), viper.GetInt("grpc_port"))
	address := fmt.Sprintf("%s:%d", viper.GetString("http_host"), viper.GetInt("http_port"))
	verbose := viper.GetUint("verbose")
	leaseDuration := viper.GetUint("leaseDuration")
	adminGrpcOpts := make([]admin.Option, 0)
	if leaseDuration >= 15 {
		adminGrpcOpts = append(adminGrpcOpts, admin.WithLeaseTimes(time.Second*time.Duration(leaseDuration)))
	} else if leaseDuration > 0 {
		currTime := time.Now()
		log.Printf("%s[Warning] | %s | Parameter Lease Duration Set to less than 15 seconds defaulted to 30 seconds%s",
			ColorYellow, currTime.Format(time.RFC3339), ColorReset)
		leaseDuration = 30
	}
	log.Printf("%s[INFO] | %s | creating admin grpc server at %s%s", ColorGreen, time.Now().Format(time.RFC3339), grpcAddr, ColorReset)
	grpcServer, err := admin.NewGrpcAdminServer(grpcAddr, adminGrpcOpts...)
	if err != nil {
		log.Fatalf("%s[ERROR] | %s | Error creating admin grpc server: %s%s", ColorRed, time.Now().Format(time.RFC3339), err, ColorReset)
	}
	httpMiddleWares := make([]admin.MiddlewareFunc, 0)
	if verbose > 0 {
		log.Printf("%s[INFO] | %s | Verbose logging enabled%s",
			ColorGreen, time.Now().Format(time.RFC3339), ColorReset)
		httpMiddleWares = append(httpMiddleWares, logger.LoggingMiddleware())
	}
	log.Printf("%s[INFO] Creating admin http server at %s%s", ColorGreen, address, ColorReset)
	httpServer, err := admin.NewHTTPAdminServer(address, grpcServer, admin.WithMiddleWares(httpMiddleWares))
	if err != nil {
		log.Fatalf("%s[ERROR] | %s | Error creating admin server: %s%s", ColorRed, time.Now().Format(time.RFC3339), err, ColorReset)
		return
	}
	stopCxt, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	grpcErr := make(chan error, 1)
	httpErr := make(chan error, 1)
	// error handler for grpc server
	go func() {
		log.Printf("%s[INFO] | %s | gRPC server started on %s%s", ColorGreen, time.Now().Format(time.RFC3339), grpcAddr, ColorReset)
		err := grpcServer.ListenAndServe()
		if err != nil {
			grpcErr <- err
		}
	}()
	// error handler for httpServer
	go func() {
		log.Printf("%s[INFO] | %s | HTTP admin server listening on %s%s", ColorGreen, time.Now().Format(time.RFC3339), address, ColorReset)
		err := httpServer.StartAndListen()
		if err != nil {
			httpErr <- err
		}
	}()
	//log start summary of program
	log.Printf("%s[INFO] Admin service initialized | gRPC: %s | HTTP: %s | Lease: %ds | Verbose: %d%s",
		ColorCyan, grpcAddr, address, leaseDuration, verbose, ColorReset)
	// either wait for process shutdown or error on either server
	// shutdown servers gracefully
	select {
	case err := <-grpcErr:
		if err != nil {
			log.Printf("%s[ERROR] | %s | GRPC Admin Error: %s%s", ColorRed, time.Now().Format(time.RFC3339), err, ColorReset)
		}
		_ = httpServer.Close()
		return
	case err := <-httpErr:
		if err != nil {
			log.Printf("%s[ERROR] | %s | HTTP Admin Error: %s%s", ColorRed, time.Now().Format(time.RFC3339), err, ColorReset)
		}
		grpcServer.Close()
		return
	case <-stopCxt.Done():
		log.Printf("%s[INFO] Shutting down gracefully%s", ColorGreen, ColorReset)
		_ = httpServer.Close()
		grpcServer.Close()
		return
	}

}
