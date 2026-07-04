package services

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"goblog/core/models"

	"golang.org/x/crypto/bcrypt"
)

type UserService struct {
	db *sql.DB
}

func NewUserService(db *sql.DB) *UserService {
	return &UserService{db: db}
}

func (s *UserService) EnsureDefaultAdmin(ctx context.Context, name, password, mail string) error {
	var count int
	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM gb_users`).Scan(&count); err != nil {
		return err
	}
	if count > 0 {
		return nil
	}

	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return err
	}
	now := time.Now().Unix()
	_, err = s.db.ExecContext(ctx, `
		INSERT INTO gb_users (name, password, mail, screenName, created, activated, logged, role)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)
	`, name, string(hash), mail, name, now, now, 0, "administrator")
	return err
}

func (s *UserService) Authenticate(ctx context.Context, name, password string) (models.User, error) {
	user, err := s.ByName(ctx, name)
	if err != nil {
		return models.User{}, err
	}
	if err := bcrypt.CompareHashAndPassword([]byte(user.Password), []byte(password)); err != nil {
		return models.User{}, errors.New("invalid credentials")
	}
	_, _ = s.db.ExecContext(ctx, `UPDATE gb_users SET logged = ? WHERE uid = ?`, time.Now().Unix(), user.UID)
	return user, nil
}

func (s *UserService) ByName(ctx context.Context, name string) (models.User, error) {
	var u models.User
	err := s.db.QueryRowContext(ctx, `
		SELECT uid, name, password, COALESCE(mail,''), COALESCE(url,''), COALESCE(screenName,''), created, activated, logged, role, COALESCE(authCode,'')
		FROM gb_users WHERE name = ?
	`, name).Scan(&u.UID, &u.Name, &u.Password, &u.Mail, &u.URL, &u.ScreenName, &u.Created, &u.Activated, &u.Logged, &u.Role, &u.AuthCode)
	return u, err
}
