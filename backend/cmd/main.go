// 阶段一：读配置->连接数据库->建路由->r.run()
package main

import (
	"log"
	"os"
	"strconv"

	"github.com/joho/godotenv" //from dotenv import load_dotenv
	"github.com/jttt-17/feedsystem/backend/internal/config"
	"github.com/jttt-17/feedsystem/backend/internal/db"
	apphttp "github.com/jttt-17/feedsystem/backend/internal/http"
)

func main() {
	//获取环境变量 相当于load_dotenv()
	if err := godotenv.Load(); err != nil {
		log.Println(".env not found; continuing")
	}

	//加载配置
	configPath := os.Getenv("CONFIG_PATH")
	if configPath == "" {
		configPath = "configs/config.yaml"
	}
	log.Printf("Loading config from %s", configPath)
	cfg, usedDefault, err := config.LoadLocalDev(configPath)
	if err != nil {
		log.Fatalf("Failed to load config: %v", err)
	}
	if usedDefault {
		log.Printf("Config File %s not found, using default local config", configPath)
	} else {
		log.Printf("Config loaded from file: %s", configPath)
	}

	//连接数据库
	sqlDB, err := db.NewDB(cfg.Database)
	if err != nil {
		log.Fatalf("Failed to connect database: %v", err)
	}
	// if err := db.AutoMigrate(sqlDB); err != nil {
	// 	log.Fatalf("Failed to auto migrate database: %v", err)
	// }
	defer db.CloseDB(sqlDB)

	//设置路由
	r := apphttp.SetRouter(sqlDB)
	log.Printf("Server is running on port %d", cfg.Server.Port)
	if err := r.Run(":" + strconv.Itoa(cfg.Server.Port)); err != nil {
		log.Fatalf("Failed to run server: %v", err)
	}

}
