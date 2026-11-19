package repository

import (
	"errors"
	"fmt"
	"log/slog"
	"strings"

	"github.com/golang-migrate/migrate/v4"
	_ "github.com/golang-migrate/migrate/v4/database/pgx/v5"
	"github.com/golang-migrate/migrate/v4/source/iofs"

	"github.com/grbzemir/notification-system/migrations"
)

// Migrate gömülü SQL dosyalarından henüz uygulanmamış olanları sırayla çalıştırır.
func Migrate(databaseURL string) error {
	src, err := iofs.New(migrations.FS, ".")
	if err != nil {
		return fmt.Errorf("load migrations: %w", err)
	}

	// golang-migrate'in pgx v5 sürücüsü "pgx5://" şemasını bekler.
	dbURL := strings.Replace(databaseURL, "postgres://", "pgx5://", 1)

	m, err := migrate.NewWithSourceInstance("iofs", src, dbURL)
	if err != nil {
		return fmt.Errorf("init migrate: %w", err)
	}
	defer m.Close()

	if err := m.Up(); err != nil {
		if errors.Is(err, migrate.ErrNoChange) {
			slog.Info("no new migrations")
			return nil
		}
		return fmt.Errorf("apply migrations: %w", err)
	}
	return nil
}
