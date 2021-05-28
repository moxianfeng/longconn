package tools

import "os"

func GetEnv(name, defaultValue string) string {
	temp := os.Getenv(name)
	if len(temp) > 0 {
		return temp
	} else {
		return defaultValue
	}
}
