package main

import (
	"GOMinifyURL/internal/admin"
	"GOMinifyURL/internal/middleware/logger"
	"context"
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/spf13/pflag"
	"github.com/spf13/viper"
)

func main() {
	log.SetPrefix("[admin]: ")

	pflag.StringP("grpc_host", "gh", "", "Host to bind the admin grpc server to")
	pflag.IntP("grpc_port", "gp", 8083, "Port to bind the admin grpc server to")
	pflag.StringP("http_host", "h", "", "Host to bind the admin http server to")
	pflag.IntP("http_port", "h", 8080, "Port to bind the admin http server to")
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
	address := fmt.Sprintf("%s:%d", viper.GetString("host"), viper.GetInt("port"))
	verbose := viper.GetUint("verbose")
	leaseDuration := viper.GetUint("leaseDuration")
	adminGrpcOpts := make([]admin.Option, 0)
	if leaseDuration >= 15 {
		adminGrpcOpts = append(adminGrpcOpts, admin.WithLeaseTimes(time.Second*time.Duration(leaseDuration)))
	}
	grpcServer, err := admin.NewGrpcAdminServer(grpcAddr, adminGrpcOpts...)
	if err != nil {
		log.Fatal(err)
		return
	}
	httpMiddleWares := make([]admin.MiddlewareFunc, 0)
	if verbose > 0 {
		httpMiddleWares = append(httpMiddleWares, logger.LoggingMiddleware())
	}
	httpServer, err := admin.NewHTTPAdminServer(address, grpcServer, admin.WithMiddleWares(httpMiddleWares))
	if err != nil {
		log.Fatal(err)
		return
	}
	stopCxt, stopFunc := context.WithCancel(context.Background())
	grpcErr := make(chan error, 1)
	httpErr := make(chan error, 1)

	// signal handler
	go func() {
		defer stopFunc()
		sigChan := make(chan os.Signal, 1)
		signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)
		<-sigChan
	}()
	// error handler for grpc server
	go func() {
		err := grpcServer.ListenAndServe()
		if err != nil {
			grpcErr <- err
		}
	}()
	// error handler for httpServer
	go func() {
		err := httpServer.StartAndListen()
		if err != nil {
			httpErr <- err
		}
	}()
	// either wait for process shutdown or error on either server
	// shutdown servers gracefully
	select {
	case _, ok := <-grpcErr:
		if ok {
			_ = httpServer.Close()
			return
		}
		return
	case _, ok := <-httpErr:
		if ok {
			grpcServer.Close()
		}
		return
	case <-stopCxt.Done():
		_ = httpServer.Close()
		grpcServer.Close()
		return
	}

}
