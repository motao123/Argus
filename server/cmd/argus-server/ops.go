package main

import (
	"flag"
	"fmt"
	"os"

	"golang.org/x/crypto/bcrypt"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"

	"github.com/motao123/Argus/server/internal/model"
)

// 运维子命令（借鉴 komari chpasswd/disable2fa/permitPasswordLogin 逃生门）。
func runOps() {
	sub := flag.Args()[0]
	switch sub {
	case "chpasswd":
		cmdChangePassword()
	case "disable-2fa":
		cmdDisable2FA()
	default:
		fmt.Println("用法: argus-server <server|chpasswd|disable-2fa> [flags]")
		os.Exit(1)
	}
}

// cmdChangePassword 强制改密：argus-server chpasswd -d <db> -u <username> -p <newpass>
func cmdChangePassword() {
	fs := flag.NewFlagSet("chpasswd", flag.ExitOnError)
	dbPath := fs.String("d", "./data/argus.db", "数据库路径")
	username := fs.String("u", "admin", "用户名")
	password := fs.String("p", "", "新密码（>=6 位）")
	fs.Parse(flag.Args()[1:])

	if *password == "" || len(*password) < 6 {
		fmt.Println("错误: 密码至少 6 位")
		os.Exit(1)
	}
	gdb := openDB(*dbPath)
	hash, _ := bcrypt.GenerateFromPassword([]byte(*password), bcrypt.DefaultCost)
	res := gdb.Model(&model.User{}).Where("username = ?", *username).
		Update("password_hash", string(hash))
	if res.RowsAffected == 0 {
		fmt.Printf("用户 %s 不存在\n", *username)
		os.Exit(1)
	}
	fmt.Printf("✅ 已重置用户 %s 的密码\n", *username)
}

// cmdDisable2FA 强制关闭 2FA：argus-server disable-2fa -d <db> -u <username>
func cmdDisable2FA() {
	fs := flag.NewFlagSet("disable-2fa", flag.ExitOnError)
	dbPath := fs.String("d", "./data/argus.db", "数据库路径")
	username := fs.String("u", "admin", "用户名")
	fs.Parse(flag.Args()[1:])

	gdb := openDB(*dbPath)
	res := gdb.Model(&model.User{}).Where("username = ?", *username).
		Updates(map[string]any{"two_fa_enabled": false, "two_fa_secret": ""})
	if res.RowsAffected == 0 {
		fmt.Printf("用户 %s 不存在\n", *username)
		os.Exit(1)
	}
	fmt.Printf("✅ 已关闭用户 %s 的 2FA\n", *username)
}

func openDB(path string) *gorm.DB {
	gdb, err := gorm.Open(sqlite.Open(path), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		fmt.Println("打开数据库失败:", err)
		os.Exit(1)
	}
	return gdb
}
