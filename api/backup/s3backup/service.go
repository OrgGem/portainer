// Package s3backup schedules and exports Portainer configuration backups.
package s3backup

import (
	"context"
	"errors"
	"net"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"
	"unicode"

	portainer "github.com/portainer/portainer/api"
	operations "github.com/portainer/portainer/api/backup"
	"github.com/portainer/portainer/api/dataservices"
	"github.com/portainer/portainer/api/http/offlinegate"
	"github.com/rs/zerolog/log"
)

const (
	settingsBucket = "settings"
	stateKey       = "CUSTOM_S3_BACKUP"
	retryDelay     = time.Minute
	backupTimeout  = 30 * time.Minute
)

var (
	ErrBackupRunning   = errors.New("a backup is already running")
	ErrInvalidSettings = errors.New("save a valid S3 backup destination first")
	errScheduleChanged = errors.New("backup schedule changed")
	bucketPattern      = regexp.MustCompile(`^[a-z0-9][a-z0-9.-]{1,61}[a-z0-9]$`)
	regionPattern      = regexp.MustCompile(`^[a-z0-9]+(?:-[a-z0-9]+)+-[0-9]+$`)
)

// Configuration is the administrator-supplied destination and daily UTC schedule.
type Configuration struct {
	BucketName string `json:"bucketName"`
	Region     string `json:"region"`
	Prefix     string `json:"prefix"`
	Enabled    bool   `json:"enabled"`
	DailyTime  string `json:"dailyTime"`
}

// Destination identifies where a particular backup was attempted.
type Destination struct {
	BucketName string `json:"bucketName"`
	Region     string `json:"region"`
	Prefix     string `json:"prefix"`
}

// Run describes the result of the most recent attempted backup.
type Run struct {
	At          time.Time    `json:"at"`
	Status      string       `json:"status"`
	Message     string       `json:"message,omitempty"`
	Key         string       `json:"key,omitempty"`
	Destination *Destination `json:"destination,omitempty"`
}

// Settings includes the current destination and the last backup result.
type Settings struct {
	Configuration
	LastRun *Run `json:"lastRun,omitempty"`
}

// state is stored separately from the general settings object so backup results
// cannot overwrite unrelated concurrent settings changes.
type state struct {
	Configuration Configuration `json:"configuration"`
	LastRun       *Run          `json:"lastRun,omitempty"`
	LastScheduled time.Time     `json:"lastScheduled"`
	Revision      uint64        `json:"revision"`
}

// Service owns the backup scheduler and serializes scheduled and manual runs.
type Service struct {
	store         dataservices.DataStore
	createArchive func() (string, error)
	upload        func(context.Context, Configuration, string) (string, error)
	now           func() time.Time
	runMu         sync.Mutex
	wake          chan struct{}
}

func NewService(store dataservices.DataStore, gate *offlinegate.OfflineGate, filestorePath string) *Service {
	return &Service{
		store: store,
		createArchive: func() (string, error) {
			return operations.CreateBackupArchive("", gate, store, filestorePath)
		},
		upload: uploadArchive,
		now:    time.Now,
		wake:   make(chan struct{}, 1),
	}
}

func defaultState() state {
	return state{Configuration: Configuration{DailyTime: "00:00"}}
}

// Validate rejects destinations that cannot be used for an ordinary S3 bucket.
func (c Configuration) Validate() error {
	if !bucketPattern.MatchString(c.BucketName) || strings.Contains(c.BucketName, "..") ||
		strings.Contains(c.BucketName, ".-") || strings.Contains(c.BucketName, "-.") || net.ParseIP(c.BucketName) != nil {
		return errors.New("enter a valid S3 bucket name")
	}
	if len(c.Region) > 63 || !regionPattern.MatchString(c.Region) {
		return errors.New("enter a valid AWS region")
	}
	if len(c.Prefix) > 900 || strings.Trim(c.Prefix, "/") != c.Prefix || strings.TrimSpace(c.Prefix) != c.Prefix {
		return errors.New("enter a prefix without leading or trailing slashes")
	}
	for _, ch := range c.Prefix {
		if unicode.IsControl(ch) {
			return errors.New("the S3 prefix must not contain control characters")
		}
	}
	parsed, err := time.Parse("15:04", c.DailyTime)
	if err != nil || parsed.Format("15:04") != c.DailyTime {
		return errors.New("enter a daily backup time in HH:mm UTC format")
	}
	return nil
}

func (s *Service) readState() (state, error) {
	current := defaultState()
	err := s.store.Connection().GetObject(settingsBucket, []byte(stateKey), &current)
	if dataservices.IsErrObjectNotFound(err) {
		return defaultState(), nil
	}
	return current, err
}

func (s *Service) updateState(update func(*state) error) error {
	return s.store.Connection().UpdateTx(func(tx portainer.Transaction) error {
		current := defaultState()
		err := tx.GetObject(settingsBucket, []byte(stateKey), &current)
		if err != nil && !dataservices.IsErrObjectNotFound(err) {
			return err
		}
		if err := update(&current); err != nil {
			return err
		}
		return tx.UpdateObject(settingsBucket, []byte(stateKey), &current)
	})
}

// Get returns the current configuration and most recent backup result.
func (s *Service) Get() (Settings, error) {
	current, err := s.readState()
	if err != nil {
		return Settings{}, err
	}
	return Settings{Configuration: current.Configuration, LastRun: current.LastRun}, nil
}

// Save atomically changes the schedule and wakes the scheduler.
func (s *Service) Save(config Configuration) (Settings, error) {
	if err := config.Validate(); err != nil {
		return Settings{}, err
	}
	var saved Settings
	err := s.updateState(func(current *state) error {
		current.Configuration = config
		current.Revision++
		// Start from the time the schedule was saved, then catch up any later
		// scheduled slot missed while the server is unavailable.
		current.LastScheduled = s.now().UTC()
		saved = Settings{Configuration: config, LastRun: current.LastRun}
		return nil
	})
	if err != nil {
		return Settings{}, err
	}
	select {
	case s.wake <- struct{}{}:
	default:
	}
	return saved, nil
}

type scheduleRun struct {
	due      time.Time
	revision uint64
}

// Execute creates and uploads a backup using the saved destination.
func (s *Service) Execute(ctx context.Context) (Run, error) {
	return s.execute(ctx, nil)
}

func (s *Service) execute(ctx context.Context, scheduled *scheduleRun) (Run, error) {
	if !s.runMu.TryLock() {
		return Run{}, ErrBackupRunning
	}
	defer s.runMu.Unlock()

	current, err := s.readState()
	if err != nil {
		return Run{}, errors.New("unable to read backup settings")
	}
	if scheduled != nil && (current.Revision != scheduled.revision || !current.Configuration.Enabled) {
		return Run{}, errScheduleChanged
	}
	if err := current.Configuration.Validate(); err != nil {
		return Run{}, ErrInvalidSettings
	}
	if err := ctx.Err(); err != nil {
		return Run{}, errors.New("backup was cancelled")
	}

	result := Run{
		Status: "failed",
		Destination: &Destination{
			BucketName: current.Configuration.BucketName,
			Region:     current.Configuration.Region,
			Prefix:     current.Configuration.Prefix,
		},
	}
	archivePath, err := s.createArchive()
	if err != nil {
		log.Error().Err(err).Msg("unable to create S3 backup archive")
		err = errors.New("unable to create the backup archive; check the server logs")
	} else {
		defer func() {
			if removeErr := os.RemoveAll(filepath.Dir(archivePath)); removeErr != nil {
				log.Warn().Err(removeErr).Msg("unable to remove S3 backup temporary directory")
			}
		}()
		var key string
		uploadCtx, cancel := context.WithTimeout(ctx, backupTimeout)
		key, err = s.upload(uploadCtx, current.Configuration, archivePath)
		cancel()
		if err == nil {
			result.Status = "success"
			result.Key = key
		}
	}
	result.At = s.now().UTC()
	if err != nil {
		result.Message = err.Error()
	}
	// A shutdown-cancelled upload needs another attempt after restart. Credit a
	// successful upload even if shutdown began immediately after it completed.
	shutdownAborted := err != nil && ctx.Err() != nil && errors.Is(err, ctx.Err())
	if stateErr := s.updateState(func(latest *state) error {
		latest.LastRun = &result
		if scheduled != nil && latest.Revision == scheduled.revision && !shutdownAborted {
			latest.LastScheduled = scheduled.due
		}
		return nil
	}); stateErr != nil {
		return result, errors.New("backup finished but its result could not be saved")
	}
	return result, err
}

// nextScheduled returns the next due slot. A missed slot is returned immediately
// when the process restarts, and later slots remain on the original UTC schedule.
func nextScheduled(current state, now time.Time) (time.Time, error) {
	parsed, err := time.Parse("15:04", current.Configuration.DailyTime)
	if err != nil || parsed.Format("15:04") != current.Configuration.DailyTime {
		return time.Time{}, errors.New("invalid daily backup time")
	}
	now = now.UTC()
	today := time.Date(now.Year(), now.Month(), now.Day(), parsed.Hour(), parsed.Minute(), 0, 0, time.UTC)
	latestDue := today
	if now.Before(today) {
		latestDue = today.Add(-24 * time.Hour)
	}
	if current.LastScheduled.Before(latestDue) {
		return latestDue, nil
	}
	if now.Before(today) {
		return today, nil
	}
	return today.Add(24 * time.Hour), nil
}

// Start restores the schedule and stops its worker when ctx is cancelled.
func (s *Service) Start(ctx context.Context) {
	go s.schedule(ctx)
}

func (s *Service) schedule(ctx context.Context) {
	for ctx.Err() == nil {
		current, err := s.readState()
		if err != nil {
			log.Error().Msg("unable to read S3 backup schedule")
			if !s.wait(ctx, retryDelay) {
				return
			}
			continue
		}
		if !current.Configuration.Enabled {
			select {
			case <-ctx.Done():
				return
			case <-s.wake:
				continue
			}
		}
		due, err := nextScheduled(current, s.now())
		if err != nil {
			log.Error().Msg("invalid S3 backup schedule")
			if !s.wait(ctx, retryDelay) {
				return
			}
			continue
		}
		wait := due.Sub(s.now())
		if wait < 0 {
			wait = 0
		}
		timer := time.NewTimer(wait)
		select {
		case <-ctx.Done():
			timer.Stop()
			return
		case <-s.wake:
			timer.Stop()
			continue
		case <-timer.C:
		}
		_, err = s.execute(ctx, &scheduleRun{due: due, revision: current.Revision})
		if errors.Is(err, errScheduleChanged) {
			continue
		}
		if err != nil {
			log.Error().Str("reason", err.Error()).Msg("scheduled S3 backup failed")
			// A busy run or database failure must not cause a tight retry loop.
			if !s.wait(ctx, retryDelay) {
				return
			}
		}
	}
}

func (s *Service) wait(ctx context.Context, delay time.Duration) bool {
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-s.wake:
		return true
	case <-timer.C:
		return true
	}
}
