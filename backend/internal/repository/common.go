package repository

import (
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"errors"

	"github.com/go-sql-driver/mysql"

	"github.com/maket12/issues-tracker/backend/internal/domain"
)

func translate(err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, sql.ErrNoRows) {
		return domain.ErrNotFound
	}
	var me *mysql.MySQLError
	if errors.As(err, &me) && me.Number == 3819 { // CHECK (dueDate >= startDate) violated
		return domain.NewValidation("dueDate", "must not be earlier than startDate")
	}
	return err
}

func randomHex(nBytes int) (string, error) {
	b := make([]byte, nBytes)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

func nullable(s sql.NullString) *string {
	if !s.Valid {
		return nil
	}
	return &s.String
}
