/*
 *                        _oo0oo_
 *                       o8888888o
 *                       88" . "88
 *                       (| -_- |)
 *                       0\  =  /0
 *                     ___/`---'\___
 *                   .' \\|     |// '.
 *                  / \\|||  :  |||// \
 *                 / _||||| -:- |||||- \
 *                |   | \\\  - /// |   |
 *                | \_|  ''\---/''  |_/ |
 *                \  .-\__  '-'  ___/-. /
 *              ___'. .'  /--.--\  `. .'___
 *           ."" '<  `.___\_<|>_/___.' >' "".
 *          | | :  `- \`.;`\ _ /`;.`/ - ` : | |
 *          \  \ `_.   \_ __\ /__ _/   .-` /  /
 *      =====`-.____`.___ \_____/___.-`___.-'=====
 *                        `=---='
 *
 *
 *      ~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~
 *
 *            佛祖保佑       永不宕机     永无BUG
 */

package main

import (
	"os"

	"github.com/gin-gonic/gin"
	"github.com/joho/godotenv"
	"kilocli2api/Utils"
)

func main() {
	Utils.InitLoggers()

	// Load .env file
	if err := godotenv.Load(); err != nil {
		Utils.NormalLogger.Println("Warning: .env file not found, using system values. Docker user ignore this.")
	}

	if proxyURL := os.Getenv("PROXY_URL"); proxyURL != "" {
		Utils.NormalLogger.Printf("Using proxy: %s\n", proxyURL)
	}

	_, err := Utils.GetBearer()
	if err != nil {
		Utils.NormalLogger.Println("Error getting initial bearer token:", err)
		return
	}
	Utils.StartTokenRefresher()

	// Get PORT from environment variable, default to 8080
	port := os.Getenv("PORT")
	if port == "" {
		port = "4000"
	}

	ginMode := os.Getenv("GIN_MODE")
	if ginMode == "release" {
		gin.SetMode(gin.ReleaseMode)
	} else if ginMode == "debug" {
		gin.SetMode(gin.DebugMode)
	} else {
		gin.SetMode(gin.ReleaseMode)
	}

	gin.DefaultWriter = Utils.NormalLogger.Writer()
	r := gin.Default()

	setupRouter(r)

	// Start server

	Utils.NormalLogger.Printf("Server starting on :%s\n", port)
	if err := r.Run(":" + port); err != nil {
		Utils.NormalLogger.Fatal("Failed to start server:", err)
	}
}
