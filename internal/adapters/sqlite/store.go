package sqlite

import (
	"context"
	"database/sql"
	"embed"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/pressly/goose/v3"
	"github.com/sudoHG/x-monitor/internal/adapters/sqlite/sqlcgen"
	"github.com/sudoHG/x-monitor/internal/domain"
	"github.com/sudoHG/x-monitor/internal/infrastructure/config"
	"github.com/sudoHG/x-monitor/internal/ports"
	"go.uber.org/fx"
	_ "modernc.org/sqlite"
)

//go:embed migrations/*.sql
var migrationFS embed.FS

var migrationMu sync.Mutex

type Store struct {
	db      *sql.DB
	queries *sqlcgen.Queries
}

var Module = fx.Module(
	"sqlite",
	fx.Provide(
		fx.Annotate(New, fx.As(new(ports.Store))),
	),
)

func New(lifecycle fx.Lifecycle, cfg config.Config) (*Store, error) {
	if err := os.MkdirAll(filepath.Dir(cfg.Storage.Database), 0o700); err != nil {
		return nil, fmt.Errorf("create database directory: %w", err)
	}
	databaseFile, err := os.OpenFile(cfg.Storage.Database, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, fmt.Errorf("create database file: %w", err)
	}
	if err := databaseFile.Close(); err != nil {
		return nil, fmt.Errorf("close database file: %w", err)
	}
	if err := os.Chmod(cfg.Storage.Database, 0o600); err != nil {
		return nil, fmt.Errorf("secure database file: %w", err)
	}

	absolute, err := filepath.Abs(cfg.Storage.Database)
	if err != nil {
		return nil, fmt.Errorf("resolve database path: %w", err)
	}
	dsnURL := &url.URL{Scheme: "file", Path: absolute}
	dsn := dsnURL.String() + "?_pragma=foreign_keys(1)&_pragma=busy_timeout(5000)"
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("open SQLite: %w", err)
	}
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)
	if err := db.Ping(); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("ping SQLite: %w", err)
	}
	for _, statement := range []string{
		"PRAGMA journal_mode = WAL",
		"PRAGMA foreign_keys = ON",
		"PRAGMA busy_timeout = 5000",
		"PRAGMA synchronous = NORMAL",
	} {
		if _, err := db.Exec(statement); err != nil {
			_ = db.Close()
			return nil, fmt.Errorf("configure SQLite with %q: %w", statement, err)
		}
	}
	store := &Store{db: db, queries: sqlcgen.New(db)}
	if err := store.Migrate(context.Background()); err != nil {
		_ = db.Close()
		return nil, err
	}
	lifecycle.Append(fx.Hook{OnStop: func(_ context.Context) error {
		return store.Close()
	}})
	return store, nil
}

func (s *Store) Migrate(ctx context.Context) error {
	migrationMu.Lock()
	defer migrationMu.Unlock()
	goose.SetBaseFS(migrationFS)
	if err := goose.SetDialect("sqlite3"); err != nil {
		return fmt.Errorf("set goose dialect: %w", err)
	}
	if err := goose.UpContext(ctx, s.db, "migrations"); err != nil {
		return fmt.Errorf("run SQLite migrations: %w", err)
	}
	return nil
}

func (s *Store) Close() error {
	return s.db.Close()
}

func (s *Store) SyncAccounts(ctx context.Context, accounts []string) error {
	now := time.Now().UTC().UnixMilli()
	for _, account := range accounts {
		if err := s.queries.UpsertAccount(ctx, sqlcgen.UpsertAccountParams{
			Handle:      account,
			CreatedAtMs: now,
		}); err != nil {
			return fmt.Errorf("upsert account %s: %w", account, err)
		}
	}
	if len(accounts) > 0 {
		if err := s.queries.DisableMissingAccounts(ctx, accounts); err != nil {
			return fmt.Errorf("disable removed accounts: %w", err)
		}
	}
	return nil
}

func (s *Store) StartRun(ctx context.Context, run domain.Run) error {
	return s.queries.InsertRun(ctx, sqlcgen.InsertRunParams{
		ID:            run.ID,
		TriggerType:   string(run.Trigger),
		ScheduledAtMs: nullableTime(run.Scheduled),
		StartedAtMs:   run.StartedAt.UTC().UnixMilli(),
		WindowStartMs: run.WindowFrom.UTC().UnixMilli(),
		WindowEndMs:   run.WindowTo.UTC().UnixMilli(),
		Status:        string(run.Status),
	})
}

func (s *Store) CompleteRun(ctx context.Context, run domain.Run) error {
	return s.queries.CompleteRun(ctx, sqlcgen.CompleteRunParams{
		CompletedAtMs:     nullableTime(run.Completed),
		Status:            string(run.Status),
		GrokRunID:         nullableString(run.GrokRunID),
		PostsFound:        run.PostsFound,
		PostsNew:          run.PostsNew,
		NotificationsSent: run.Sent,
		ErrorCode:         nullableString(run.ErrorCode),
		ErrorMessage:      nullableString(run.Error),
		ID:                run.ID,
	})
}

func (s *Store) LastRun(ctx context.Context) (*domain.Run, error) {
	row, err := s.queries.GetLastRun(ctx)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get last run: %w", err)
	}
	result := mapRun(row)
	return &result, nil
}

func (s *Store) LastSuccessfulRun(ctx context.Context) (*domain.Run, error) {
	row, err := s.queries.GetLastSuccessfulRun(ctx)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get last successful run: %w", err)
	}
	result := mapRun(row)
	return &result, nil
}

func (s *Store) InsertPostsAndDeliveries(
	ctx context.Context,
	runID string,
	posts []domain.Post,
	destination string,
	now time.Time,
	deliveryCutoff *time.Time,
) ([]domain.Post, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("begin posts transaction: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	queries := s.queries.WithTx(tx)
	inserted := make([]domain.Post, 0, len(posts))
	for _, post := range posts {
		uncertainty, err := json.Marshal(post.Uncertainties)
		if err != nil {
			return nil, fmt.Errorf("encode post uncertainties: %w", err)
		}
		result, err := queries.InsertPost(ctx, sqlcgen.InsertPostParams{
			StatusUrl:       post.StatusURL,
			StatusID:        post.StatusID,
			AccountHandle:   post.Account,
			ContentType:     string(post.ContentType),
			PublishedAtMs:   nullableTime(post.PublishedAt),
			OriginalText:    nullableString(post.OriginalText),
			SummaryZh:       post.SummaryZH,
			UncertaintyJson: string(uncertainty),
			FirstSeenRunID:  runID,
			FirstSeenAtMs:   now.UTC().UnixMilli(),
			RawJson:         post.RawJSON,
		})
		if err != nil {
			return nil, fmt.Errorf("insert post %s: %w", post.StatusURL, err)
		}
		affected, err := result.RowsAffected()
		if err != nil {
			return nil, fmt.Errorf("read insert result for %s: %w", post.StatusURL, err)
		}
		if affected == 0 {
			continue
		}
		inserted = append(inserted, post)
		shouldDeliver := deliveryCutoff == nil ||
			(post.PublishedAt != nil && !post.PublishedAt.Before(*deliveryCutoff))
		if shouldDeliver {
			if err := queries.InsertDelivery(ctx, sqlcgen.InsertDeliveryParams{
				StatusUrl:   post.StatusURL,
				Destination: destination,
				CreatedAtMs: now.UTC().UnixMilli(),
				UpdatedAtMs: now.UTC().UnixMilli(),
			}); err != nil {
				return nil, fmt.Errorf("insert delivery for %s: %w", post.StatusURL, err)
			}
		}
	}
	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("commit posts transaction: %w", err)
	}
	return inserted, nil
}

func (s *Store) ClaimNextDelivery(
	ctx context.Context,
	owner string,
	readyAt time.Time,
	expiresAt time.Time,
) (*domain.Delivery, error) {
	now := time.Now().UTC()
	row, err := s.queries.ClaimNextDelivery(ctx, sqlcgen.ClaimNextDeliveryParams{
		ProcessingOwner:       nullableString(owner),
		ProcessingExpiresAtMs: nullableTime(&expiresAt),
		UpdatedAtMs:           now.UnixMilli(),
		ReadyAtMs:             nullableTime(&readyAt),
		ExpiredAtMs:           nullableTime(&readyAt),
	})
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("claim delivery: %w", err)
	}
	postRow, err := s.queries.GetPostByStatusURL(ctx, row.StatusUrl)
	if err != nil {
		return nil, fmt.Errorf("load claimed delivery post: %w", err)
	}
	delivery := mapDelivery(row, mapPost(postRow))
	return &delivery, nil
}

func (s *Store) MarkDeliverySent(ctx context.Context, id, messageID int64, sentAt time.Time) error {
	return s.queries.MarkDeliverySent(ctx, sqlcgen.MarkDeliverySentParams{
		TelegramMessageID: nullableInt(messageID),
		SentAtMs:          nullableTime(&sentAt),
		UpdatedAtMs:       sentAt.UTC().UnixMilli(),
		ID:                id,
	})
}

func (s *Store) MarkDeliveryRetry(ctx context.Context, id int64, next time.Time, cause string, now time.Time) error {
	return s.queries.MarkDeliveryRetry(ctx, sqlcgen.MarkDeliveryRetryParams{
		NextAttemptAtMs: nullableTime(&next),
		LastError:       nullableString(cause),
		UpdatedAtMs:     now.UTC().UnixMilli(),
		ID:              id,
	})
}

func (s *Store) MarkDeliveryDead(ctx context.Context, id int64, cause string, now time.Time) error {
	return s.queries.MarkDeliveryDead(ctx, sqlcgen.MarkDeliveryDeadParams{
		LastError:   nullableString(cause),
		UpdatedAtMs: now.UTC().UnixMilli(),
		ID:          id,
	})
}

func (s *Store) RecoverExpiredDeliveries(ctx context.Context, now time.Time) error {
	return s.queries.RecoverExpiredDeliveries(ctx, nullableTime(&now))
}

func (s *Store) ServiceStatus(ctx context.Context) (domain.ServiceStatus, error) {
	last, err := s.LastRun(ctx)
	if err != nil {
		return domain.ServiceStatus{}, err
	}
	success, err := s.LastSuccessfulRun(ctx)
	if err != nil {
		return domain.ServiceStatus{}, err
	}
	counts, err := s.queries.GetDeliveryCounts(ctx)
	if err != nil {
		return domain.ServiceStatus{}, fmt.Errorf("get delivery counts: %w", err)
	}
	status := domain.ServiceStatus{
		LastRun:           last,
		LastSuccessfulRun: success,
		PendingDeliveries: counts.PendingCount,
		RetryDeliveries:   counts.RetryCount,
		DeadDeliveries:    counts.DeadCount,
	}
	if success != nil && success.Completed != nil {
		value := success.Completed.UTC()
		status.LastSuccessfulAtUTC = &value
	}
	return status, nil
}

func mapRun(row sqlcgen.Run) domain.Run {
	return domain.Run{
		ID:         row.ID,
		Trigger:    domain.TriggerType(row.TriggerType),
		Scheduled:  timeFromNull(row.ScheduledAtMs),
		StartedAt:  time.UnixMilli(row.StartedAtMs).UTC(),
		Completed:  timeFromNull(row.CompletedAtMs),
		WindowFrom: time.UnixMilli(row.WindowStartMs).UTC(),
		WindowTo:   time.UnixMilli(row.WindowEndMs).UTC(),
		Status:     domain.RunStatus(row.Status),
		GrokRunID:  row.GrokRunID.String,
		PostsFound: row.PostsFound,
		PostsNew:   row.PostsNew,
		Sent:       row.NotificationsSent,
		ErrorCode:  row.ErrorCode.String,
		Error:      row.ErrorMessage.String,
	}
}

func mapPost(row sqlcgen.Post) domain.Post {
	var uncertainties []string
	_ = json.Unmarshal([]byte(row.UncertaintyJson), &uncertainties)
	return domain.Post{
		StatusURL:     row.StatusUrl,
		StatusID:      row.StatusID,
		Account:       row.AccountHandle,
		ContentType:   domain.ContentType(row.ContentType),
		PublishedAt:   timeFromNull(row.PublishedAtMs),
		OriginalText:  row.OriginalText.String,
		SummaryZH:     row.SummaryZh,
		Uncertainties: uncertainties,
		RawJSON:       row.RawJson,
	}
}

func mapDelivery(row sqlcgen.Delivery, post domain.Post) domain.Delivery {
	return domain.Delivery{
		ID:                  row.ID,
		Post:                post,
		Destination:         row.Destination,
		State:               domain.DeliveryState(row.State),
		Attempts:            row.Attempts,
		NextAttemptAt:       timeFromNull(row.NextAttemptAtMs),
		ProcessingOwner:     row.ProcessingOwner.String,
		ProcessingExpiresAt: timeFromNull(row.ProcessingExpiresAtMs),
		TelegramMessageID:   row.TelegramMessageID.Int64,
		SentAt:              timeFromNull(row.SentAtMs),
		LastError:           row.LastError.String,
		CreatedAt:           time.UnixMilli(row.CreatedAtMs).UTC(),
		UpdatedAt:           time.UnixMilli(row.UpdatedAtMs).UTC(),
	}
}

func nullableTime(value *time.Time) sql.NullInt64 {
	if value == nil {
		return sql.NullInt64{}
	}
	return sql.NullInt64{Int64: value.UTC().UnixMilli(), Valid: true}
}

func nullableString(value string) sql.NullString {
	return sql.NullString{String: value, Valid: value != ""}
}

func nullableInt(value int64) sql.NullInt64 {
	return sql.NullInt64{Int64: value, Valid: value != 0}
}

func timeFromNull(value sql.NullInt64) *time.Time {
	if !value.Valid {
		return nil
	}
	result := time.UnixMilli(value.Int64).UTC()
	return &result
}

var _ ports.Store = (*Store)(nil)
