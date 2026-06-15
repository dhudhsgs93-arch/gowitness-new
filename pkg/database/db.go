package database

import (
	"errors"
	"fmt"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	"github.com/glebarez/sqlite"
	"golang.org/x/net/publicsuffix"
	"github.com/sensepost/gowitness/pkg/models"
	"gorm.io/driver/mysql"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

// Connection returns a Database connection based on a URI
func Connection(uri string, shouldExist, debug bool) (*gorm.DB, error) {
	var err error
	var c *gorm.DB

	db, err := url.Parse(uri)
	if err != nil {
		return nil, err
	}

	var config = &gorm.Config{}
	if debug {
		config.Logger = logger.Default.LogMode(logger.Info)
	} else {
		config.Logger = logger.Default.LogMode(logger.Error)
	}

	switch db.Scheme {
	case "sqlite":
		if shouldExist {
			dbpath := filepath.Join(db.Host, db.Path)
			dbpath = filepath.Clean(dbpath)

			if _, err := os.Stat(dbpath); os.IsNotExist(err) {
				return nil, fmt.Errorf("sqlite database file does not exist: %s", dbpath)
			} else if err != nil {
				return nil, fmt.Errorf("error checking sqlite database file: %w", err)
			}
		}

		c, err = gorm.Open(sqlite.Open(db.Host+db.Path+"?cache=shared"), config)
		if err != nil {
			return nil, err
		}
		c.Exec("PRAGMA foreign_keys = ON")
		// durability + concurrency: WAL survives crashes far better than the
		// default DELETE journal, NORMAL sync is safe under WAL, busy_timeout
		// avoids "database is locked" under concurrent writers, and
		// autocheckpoint keeps the -wal file from growing unbounded.
		c.Exec("PRAGMA journal_mode = WAL")
		c.Exec("PRAGMA synchronous = NORMAL")
		c.Exec("PRAGMA busy_timeout = 5000")
		c.Exec("PRAGMA wal_autocheckpoint = 1000")
	case "postgres":
		dsn, err := convertPostgresURItoDSN(uri)
		if err != nil {
			return nil, err
		}
		c, err = gorm.Open(postgres.Open(dsn), config)
		if err != nil {
			return nil, err
		}
	case "mysql":
		dsn, err := convertMySQLURItoDSN(uri)
		if err != nil {
			return nil, err
		}
		c, err = gorm.Open(mysql.Open(dsn), config)
		if err != nil {
			return nil, err
		}
	default:
		return nil, errors.New("invalid db uri scheme")
	}

	// run database migrations on the connection
	if err := c.AutoMigrate(
		&models.Result{},
		&models.TLS{},
		&models.TLSSanList{},
		&models.Technology{},
		&models.Header{},
		&models.NetworkLog{},
		&models.ConsoleLog{},
		&models.Cookie{},
		&models.Review{},
		&models.TrashedHost{},
	); err != nil {
		return nil, err
	}

	// Backfill hostname column for existing results
	backfillHostnames(c)

	return c, nil
}

// backfillHostnames populates the hostname and root_domain columns for results that don't have them
func backfillHostnames(db *gorm.DB) {
	var count int64
	db.Model(&models.Result{}).Where("(hostname = '' OR root_domain = '') AND url != ''").Count(&count)
	if count == 0 {
		return
	}

	type idURL struct {
		ID  uint
		URL string
	}

	batchSize := 500
	offset := 0
	for {
		var rows []idURL
		db.Model(&models.Result{}).Select("id, url").
			Where("(hostname = '' OR root_domain = '') AND url != ''").
			Limit(batchSize).Offset(offset).Find(&rows)

		if len(rows) == 0 {
			break
		}

		for _, row := range rows {
			h := extractHostname(row.URL)
			rd := ExtractRootDomain(h)
			db.Model(&models.Result{}).Where("id = ?", row.ID).Updates(map[string]interface{}{
				"hostname":    h,
				"root_domain": rd,
			})
		}

		offset += batchSize
	}
}

// extractHostname extracts and normalizes a hostname from a URL
func extractHostname(rawURL string) string {
	rawURL = strings.TrimSpace(rawURL)
	if rawURL == "" {
		return ""
	}
	if !strings.Contains(rawURL, "://") {
		rawURL = "https://" + rawURL
	}
	u, err := url.Parse(rawURL)
	if err != nil {
		return ""
	}
	return strings.ToLower(u.Hostname())
}

// ExtractRootDomain returns the eTLD+1 (registrable domain) from a hostname.
// e.g. "api.dlive.tv" -> "dlive.tv", "192.168.1.1" -> "192.168.1.1"
func ExtractRootDomain(hostname string) string {
	if hostname == "" {
		return ""
	}
	// If it's an IP address, return as-is
	if net.ParseIP(hostname) != nil {
		return hostname
	}
	rd, err := publicsuffix.EffectiveTLDPlusOne(hostname)
	if err != nil {
		// Fallback: return hostname as-is
		return hostname
	}
	return rd
}

func convertMySQLURItoDSN(uri string) (string, error) {
	parsed, err := url.Parse(uri)
	if err != nil {
		return "", err
	}

	user := parsed.User.Username()
	pass, _ := parsed.User.Password()
	host := parsed.Host
	dbname := strings.TrimPrefix(parsed.Path, "/")

	// Handle "tcp(...)"
	if strings.HasPrefix(host, "tcp(") && strings.HasSuffix(host, ")") {
		host = strings.TrimPrefix(host, "tcp(")
		host = strings.TrimSuffix(host, ")")
	}

	// Default port
	if !strings.Contains(host, ":") {
		host = host + ":3306"
	}

	dsn := fmt.Sprintf(
		"%s:%s@tcp(%s)/%s?charset=utf8mb4&parseTime=True&loc=Local",
		user, pass, host, dbname,
	)

	return dsn, nil
}

func convertPostgresURItoDSN(uri string) (string, error) {
	parsed, err := url.Parse(uri)
	if err != nil {
		return "", err
	}

	user := parsed.User.Username()
	pass, _ := parsed.User.Password()
	host := parsed.Hostname()
	port := parsed.Port()
	if port == "" {
		port = "5432"
	}

	dbname := strings.TrimPrefix(parsed.Path, "/")

	// Start building the DSN
	dsn := fmt.Sprintf(
		"host=%s user=%s password=%s dbname=%s port=%s",
		host, user, pass, dbname, port,
	)

	// Add query params from URI
	query := parsed.Query()
	for key, values := range query {
		// Only take the first value per key
		dsn += fmt.Sprintf(" %s=%s", key, values[0])
	}

	return dsn, nil
}
