package utils

import "fmt"

func BuildPostgresDSN(user, password, hostPort string, dbName string, sslMode string) string {
	return fmt.Sprintf("postgres://%s:%s@%s/%s?sslmode=%s",
		user, password, hostPort, dbName, sslMode)
}
