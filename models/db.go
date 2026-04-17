package models

import (
	"log"

	"golang.org/x/crypto/bcrypt"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

// DB 全局变量，整个包都能访问
var DB *gorm.DB

func Init() {
	// 打开数据库
	var err error
	// SQLite: 低并发场景优先稳定性，启用 WAL 与 busy_timeout。
	DB, err = gorm.Open(sqlite.Open("cc.db?_foreign_keys=on&_journal_mode=WAL&_busy_timeout=5000"), &gorm.Config{})
	if err != nil {
		log.Fatal("failed to connect database:", err)
	}

	// SQLite 在本项目无高 QPS 诉求，单连接可显著降低锁竞争和慢 SQL 概率。
	sqlDB, err := DB.DB()
	if err != nil {
		log.Fatal("failed to get raw sql db:", err)
	}
	sqlDB.SetMaxOpenConns(1)
	sqlDB.SetMaxIdleConns(1)

	// 自动迁移所有表结构
	err = DB.AutoMigrate(
		&User{},
		&Room{},
		&Node{},
		&Exam{},
		&Alert{},
	)
	if err != nil {
		log.Fatal("failed to migrate database:", err)
	}

	// 创建（或确保存在）部分索引/组合索引。
	// 说明：部分唯一索引（partial unique index）无法仅靠 gorm tag 完整表达，因此这里用 Exec。
	if err := EnsureSQLiteIndexes(DB); err != nil {
		log.Fatal("failed to ensure sqlite indexes:", err)
	}

	// 初始化默认数据
	initDefaultUser()
}

// EnsureSQLiteIndexes 创建 SQLite 特有（或 gorm 不易表达）的索引。
func EnsureSQLiteIndexes(db *gorm.DB) error {
	if db == nil {
		return nil
	}

	// 业务约束：同一节点同一时刻仅允许一场“进行中考试”。
	// end_time 为 NULL 表示进行中。
	if err := db.Exec("CREATE UNIQUE INDEX IF NOT EXISTS idx_exams_node_active ON exams(node_id) WHERE end_time IS NULL AND node_id IS NOT NULL;").Error; err != nil {
		return err
	}

	// 节点清理任务：按状态+心跳时间过滤。
	if err := db.Exec("CREATE INDEX IF NOT EXISTS idx_nodes_status_heartbeat ON nodes(status, last_heartbeat_at);").Error; err != nil {
		return err
	}

	// 告警类型约束：在 SQLite 用触发器实现白名单校验。
	// 兼容既有枚举与节点 class_names 直传值。
	if err := db.Exec("DROP TRIGGER IF EXISTS trg_alerts_type_check_insert;").Error; err != nil {
		return err
	}
	if err := db.Exec("CREATE TRIGGER IF NOT EXISTS trg_alerts_type_check_insert BEFORE INSERT ON alerts FOR EACH ROW WHEN NEW.type NOT IN ('phone_cheating','look_around','whispering','leave_sheet','stand_up','other','front','head','limb','normal','sleep','stand','unknown') BEGIN SELECT RAISE(ABORT, 'invalid alert type'); END;").Error; err != nil {
		return err
	}
	if err := db.Exec("DROP TRIGGER IF EXISTS trg_alerts_type_check_update;").Error; err != nil {
		return err
	}
	if err := db.Exec("CREATE TRIGGER IF NOT EXISTS trg_alerts_type_check_update BEFORE UPDATE OF type ON alerts FOR EACH ROW WHEN NEW.type NOT IN ('phone_cheating','look_around','whispering','leave_sheet','stand_up','other','front','head','limb','normal','sleep','stand','unknown') BEGIN SELECT RAISE(ABORT, 'invalid alert type'); END;").Error; err != nil {
		return err
	}

	return nil
}

func initDefaultUser() {
	// 1. 检查并创建 admin
	var admin User
	if err := DB.Where("username = ?", "admin").First(&admin).Error; err == gorm.ErrRecordNotFound {
		hashed, err := bcrypt.GenerateFromPassword([]byte("admin"), bcrypt.DefaultCost)
		if err != nil {
			log.Fatal("failed to hash admin password:", err)
		}
		DB.Create(&User{Username: "admin", Password: string(hashed), Role: Admin})
	}
	// 2. 检查并创建 test
	var test User
	if err := DB.Where("username = ?", "test").First(&test).Error; err == gorm.ErrRecordNotFound {
		hashed, err := bcrypt.GenerateFromPassword([]byte("test"), bcrypt.DefaultCost)
		if err != nil {
			log.Fatal("failed to hash test password:", err)
		}
		DB.Create(&User{Username: "test", Password: string(hashed), Role: Proctor})
	}
}
