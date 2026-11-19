package s3backup

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/portainer/portainer/api/datastore"
	"github.com/portainer/portainer/api/http/offlinegate"
	"github.com/stretchr/testify/require"
)

func newServiceTestStore(t *testing.T) *datastore.Store {
	t.Helper()
	_, store := datastore.MustNewTestStore(t, true, false)
	return store
}

func testConfiguration() Configuration {
	return Configuration{
		BucketName: "portainer-backups",
		Region:     "ap-southeast-1",
		Prefix:     "portainer/daily",
		Enabled:    true,
		DailyTime:  "04:15",
	}
}

func newServiceWithArchive(t *testing.T, store *datastore.Store) *Service {
	t.Helper()
	service := NewService(store, offlinegate.NewOfflineGate(), t.TempDir())
	workRoot := t.TempDir()
	service.createArchive = func() (string, error) {
		parent, err := os.MkdirTemp(workRoot, "backup-workspace-")
		if err != nil {
			return "", err
		}
		path := filepath.Join(parent, "backup.tar.gz")
		if err := os.WriteFile(path, []byte("test-archive"), 0600); err != nil {
			return "", err
		}
		return path, nil
	}
	return service
}

func waitForLastRun(t *testing.T, service *Service, status, bucketName string) Settings {
	t.Helper()
	for attempt := 0; attempt < 500; attempt++ {
		settings, err := service.Get()
		if err != nil {
			t.Fatalf("read backup status: %v", err)
		}
		if settings.LastRun != nil && settings.LastRun.Status == status &&
			settings.LastRun.Destination != nil && settings.LastRun.Destination.BucketName == bucketName {
			return settings
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("backup with status %q for bucket %q did not finish", status, bucketName)
	return Settings{}
}

func TestConfigurationValidate(t *testing.T) {
	t.Parallel()

	valid := testConfiguration()
	require.NoError(t, valid.Validate())

	tests := []struct {
		name   string
		mutate func(*Configuration)
	}{
		{name: "empty bucket", mutate: func(c *Configuration) { c.BucketName = "" }},
		{name: "ip bucket name", mutate: func(c *Configuration) { c.BucketName = "192.0.2.1" }},
		{name: "bad region", mutate: func(c *Configuration) { c.Region = "" }},
		{name: "leading prefix slash", mutate: func(c *Configuration) { c.Prefix = "/backups" }},
		{name: "trailing prefix slash", mutate: func(c *Configuration) { c.Prefix = "backups/" }},
		{name: "control character in prefix", mutate: func(c *Configuration) { c.Prefix = "backups\nnext" }},
		{name: "invalid time", mutate: func(c *Configuration) { c.DailyTime = "24:00" }},
		{name: "noncanonical time", mutate: func(c *Configuration) { c.DailyTime = "4:15" }},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			config := valid
			test.mutate(&config)
			require.Error(t, config.Validate())
		})
	}
}

func TestSavePersistsConfigurationAcrossServiceInstances(t *testing.T) {
	t.Parallel()

	store := newServiceTestStore(t)
	service := NewService(store, offlinegate.NewOfflineGate(), t.TempDir())
	config := testConfiguration()

	saved, err := service.Save(config)
	require.NoError(t, err)
	require.Equal(t, config, saved.Configuration)

	restartedService := NewService(store, offlinegate.NewOfflineGate(), t.TempDir())
	loaded, err := restartedService.Get()
	require.NoError(t, err)
	require.Equal(t, config, loaded.Configuration)
	require.Nil(t, loaded.LastRun)
}

func TestExecuteUsesSavedDestinationAndRecordsSuccessfulRun(t *testing.T) {
	t.Parallel()

	store := newServiceTestStore(t)
	service := newServiceWithArchive(t, store)
	config := testConfiguration()
	_, err := service.Save(config)
	require.NoError(t, err)

	fixedTime := time.Date(2026, time.November, 19, 7, 30, 0, 0, time.UTC)
	service.now = func() time.Time { return fixedTime }
	var uploadedConfig Configuration
	var uploadedBody []byte
	service.upload = func(_ context.Context, destination Configuration, path string) (string, error) {
		uploadedConfig = destination
		var readErr error
		uploadedBody, readErr = os.ReadFile(path)
		return "portainer/daily/backup.tar.gz", readErr
	}

	result, err := service.Execute(context.Background())
	require.NoError(t, err)
	require.Equal(t, config, uploadedConfig)
	require.Equal(t, []byte("test-archive"), uploadedBody)
	require.Equal(t, "success", result.Status)
	require.Equal(t, "portainer/daily/backup.tar.gz", result.Key)
	require.Equal(t, fixedTime, result.At)
	require.Equal(t, &Destination{
		BucketName: config.BucketName,
		Region:     config.Region,
		Prefix:     config.Prefix,
	}, result.Destination)

	settings, err := service.Get()
	require.NoError(t, err)
	require.Equal(t, &result, settings.LastRun)
}

func TestExecuteRecordsFailureAndCleansTemporaryArchive(t *testing.T) {
	t.Parallel()

	store := newServiceTestStore(t)
	service := newServiceWithArchive(t, store)
	require.NoError(t, service.updateState(func(current *state) error {
		current.Configuration = testConfiguration()
		return nil
	}))
	parent := ""
	workRoot := t.TempDir()
	service.createArchive = func() (string, error) {
		var err error
		parent, err = os.MkdirTemp(workRoot, "backup-workspace-")
		if err != nil {
			return "", err
		}
		path := filepath.Join(parent, "backup.tar.gz")
		if err := os.WriteFile(path, []byte("archive"), 0600); err != nil {
			return "", err
		}
		return path, nil
	}
	uploadFailure := errors.New("unable to upload")
	service.upload = func(context.Context, Configuration, string) (string, error) {
		return "", uploadFailure
	}

	result, err := service.Execute(context.Background())
	require.ErrorIs(t, err, uploadFailure)
	require.Equal(t, "failed", result.Status)
	require.Equal(t, uploadFailure.Error(), result.Message)
	require.Equal(t, &Destination{
		BucketName: testConfiguration().BucketName,
		Region:     testConfiguration().Region,
		Prefix:     testConfiguration().Prefix,
	}, result.Destination)
	_, err = os.Stat(parent)
	require.ErrorIs(t, err, os.ErrNotExist)

	settings, err := service.Get()
	require.NoError(t, err)
	require.Equal(t, &result, settings.LastRun)
}

func TestExecuteRejectsUnconfiguredDestination(t *testing.T) {
	t.Parallel()

	store := newServiceTestStore(t)
	service := newServiceWithArchive(t, store)
	result, err := service.Execute(context.Background())
	require.ErrorIs(t, err, ErrInvalidSettings)
	require.Empty(t, result.Status)
}

func TestExecuteSerializesConcurrentRuns(t *testing.T) {
	t.Parallel()

	store := newServiceTestStore(t)
	service := newServiceWithArchive(t, store)
	_, err := service.Save(testConfiguration())
	require.NoError(t, err)

	uploadStarted := make(chan struct{})
	allowUpload := make(chan struct{})
	service.upload = func(context.Context, Configuration, string) (string, error) {
		close(uploadStarted)
		<-allowUpload
		return "backup.tar.gz", nil
	}

	firstRun := make(chan error, 1)
	go func() {
		_, err := service.Execute(context.Background())
		firstRun <- err
	}()

	select {
	case <-uploadStarted:
	case <-time.After(5 * time.Second):
		t.Fatal("first upload did not start")
	}
	_, err = service.Execute(context.Background())
	require.ErrorIs(t, err, ErrBackupRunning)
	close(allowUpload)

	select {
	case err := <-firstRun:
		require.NoError(t, err)
	case <-time.After(5 * time.Second):
		t.Fatal("first upload did not finish")
	}
}

func TestManualRunKeepsItsDestinationWhenSettingsChange(t *testing.T) {
	tests := []struct {
		name     string
		firstErr error
	}{
		{name: "success"},
		{name: "failure", firstErr: errors.New("temporary upload failure")},
	}

	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			store := newServiceTestStore(t)
			service := newServiceWithArchive(t, store)
			fixedTime := time.Date(2026, time.November, 20, 8, 30, 0, 0, time.UTC)
			service.now = func() time.Time { return fixedTime }
			configA := testConfiguration()
			_, err := service.Save(configA)
			require.NoError(t, err)

			uploadStarted := make(chan struct{})
			allowFirstUpload := make(chan struct{})
			observedDestinations := make(chan Configuration, 2)
			var uploadCalls atomic.Int32
			service.upload = func(_ context.Context, destination Configuration, _ string) (string, error) {
				call := uploadCalls.Add(1)
				observedDestinations <- destination
				if call == 1 {
					close(uploadStarted)
					<-allowFirstUpload
					if test.firstErr != nil {
						return "", test.firstErr
					}
				}
				return "backup.tar.gz", nil
			}

			type executeResult struct {
				run Run
				err error
			}
			firstResult := make(chan executeResult, 1)
			go func() {
				run, err := service.Execute(context.Background())
				firstResult <- executeResult{run: run, err: err}
			}()
			select {
			case <-uploadStarted:
			case <-time.After(5 * time.Second):
				t.Fatal("manual backup did not reach the blocked upload")
			}
			require.Equal(t, configA, <-observedDestinations)

			configB := configA
			configB.BucketName = "portainer-archive-b"
			configB.Prefix = "portainer/next"
			_, err = service.Save(configB)
			require.NoError(t, err)
			stateAfterSave, err := service.readState()
			require.NoError(t, err)
			require.Equal(t, fixedTime, stateAfterSave.LastScheduled)

			close(allowFirstUpload)
			var first executeResult
			select {
			case first = <-firstResult:
			case <-time.After(5 * time.Second):
				t.Fatal("manual backup did not finish after the upload was released")
			}
			if test.firstErr != nil {
				require.ErrorIs(t, first.err, test.firstErr)
				require.Equal(t, "failed", first.run.Status)
			} else {
				require.NoError(t, first.err)
				require.Equal(t, "success", first.run.Status)
			}
			require.Equal(t, &Destination{BucketName: configA.BucketName, Region: configA.Region, Prefix: configA.Prefix}, first.run.Destination)

			settings, err := service.Get()
			require.NoError(t, err)
			require.Equal(t, configB, settings.Configuration)
			require.Equal(t, &first.run, settings.LastRun)
			stateAfterRun, err := service.readState()
			require.NoError(t, err)
			require.Equal(t, stateAfterSave.LastScheduled, stateAfterRun.LastScheduled)

			nextRun, err := service.Execute(context.Background())
			require.NoError(t, err)
			require.Equal(t, "success", nextRun.Status)
			require.Equal(t, &Destination{BucketName: configB.BucketName, Region: configB.Region, Prefix: configB.Prefix}, nextRun.Destination)
			require.Equal(t, configB, <-observedDestinations)
		})
	}
}

func TestNextScheduledUsesUTCAndCatchesUpMissedSlot(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name          string
		now           time.Time
		lastScheduled time.Time
		want          time.Time
	}{
		{
			name:          "missed daily slot runs immediately",
			now:           time.Date(2026, time.November, 19, 4, 30, 0, 0, time.FixedZone("local", 7*60*60)),
			lastScheduled: time.Date(2026, time.November, 18, 4, 15, 0, 0, time.UTC),
			want:          time.Date(2026, time.November, 19, 4, 15, 0, 0, time.UTC),
		},
		{
			name:          "future daily slot remains on UTC day",
			now:           time.Date(2026, time.November, 19, 3, 0, 0, 0, time.UTC),
			lastScheduled: time.Date(2026, time.November, 18, 4, 15, 0, 0, time.UTC),
			want:          time.Date(2026, time.November, 19, 4, 15, 0, 0, time.UTC),
		},
		{
			name:          "completed slot schedules tomorrow",
			now:           time.Date(2026, time.November, 19, 4, 30, 0, 0, time.UTC),
			lastScheduled: time.Date(2026, time.November, 19, 4, 15, 0, 0, time.UTC),
			want:          time.Date(2026, time.November, 20, 4, 15, 0, 0, time.UTC),
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			current := state{
				Configuration: Configuration{DailyTime: "04:15", Enabled: true},
				LastScheduled: test.lastScheduled,
			}
			due, err := nextScheduled(current, test.now)
			require.NoError(t, err)
			require.Equal(t, test.want, due)
		})
	}
}

func TestScheduleRunsMissedSlotAfterRestart(t *testing.T) {
	t.Parallel()

	store := newServiceTestStore(t)
	service := newServiceWithArchive(t, store)
	fixedTime := time.Date(2026, time.November, 19, 4, 30, 0, 0, time.UTC)
	service.now = func() time.Time { return fixedTime }
	config := testConfiguration()
	config.DailyTime = "04:15"
	_, err := service.Save(config)
	require.NoError(t, err)
	require.NoError(t, service.updateState(func(current *state) error {
		current.LastScheduled = fixedTime.Add(-24 * time.Hour)
		return nil
	}))

	uploaded := make(chan Configuration, 1)
	service.upload = func(_ context.Context, destination Configuration, _ string) (string, error) {
		uploaded <- destination
		return "backup.tar.gz", nil
	}

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		service.schedule(ctx)
		close(done)
	}()

	select {
	case got := <-uploaded:
		require.Equal(t, config, got)
	case <-time.After(5 * time.Second):
		cancel()
		<-done
		t.Fatal("scheduler did not run the missed UTC slot after restart")
	}
	cancel()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("scheduler did not stop after cancellation")
	}

	settings, err := service.Get()
	require.NoError(t, err)
	require.NotNil(t, settings.LastRun)
	require.Equal(t, "success", settings.LastRun.Status)
	current, err := service.readState()
	require.NoError(t, err)
	require.Equal(t, fixedTime.Add(-15*time.Minute), current.LastScheduled)
	require.Equal(t, &Destination{
		BucketName: config.BucketName,
		Region:     config.Region,
		Prefix:     config.Prefix,
	}, settings.LastRun.Destination)
}

func TestScheduledRunKeepsStartedDestinationWhenSettingsChange(t *testing.T) {
	tests := []struct {
		name     string
		firstErr error
		status   string
	}{
		{name: "success", status: "success"},
		{name: "failure", firstErr: errors.New("temporary upload failure"), status: "failed"},
	}
	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			store := newServiceTestStore(t)
			service := newServiceWithArchive(t, store)
			fixedTime := time.Date(2026, time.November, 20, 4, 30, 0, 0, time.UTC)
			service.now = func() time.Time { return fixedTime }
			configA := testConfiguration()
			configA.DailyTime = "04:15"
			_, err := service.Save(configA)
			require.NoError(t, err)
			lastScheduledBeforeA := fixedTime.Add(-24 * time.Hour)
			require.NoError(t, service.updateState(func(current *state) error {
				current.LastScheduled = lastScheduledBeforeA
				return nil
			}))

			startedDestination := make(chan Configuration, 1)
			allowUploadToFinish := make(chan struct{})
			observedDestinations := make(chan Configuration, 2)
			var uploadCalls atomic.Int32
			service.upload = func(_ context.Context, destination Configuration, _ string) (string, error) {
				call := uploadCalls.Add(1)
				observedDestinations <- destination
				if call == 1 {
					startedDestination <- destination
					<-allowUploadToFinish
					if test.firstErr != nil {
						return "", test.firstErr
					}
				}
				return "backup.tar.gz", nil
			}

			ctx, cancel := context.WithCancel(context.Background())
			done := make(chan struct{})
			go func() {
				service.schedule(ctx)
				close(done)
			}()
			select {
			case got := <-startedDestination:
				require.Equal(t, configA, got)
			case <-time.After(5 * time.Second):
				cancel()
				<-done
				t.Fatal("scheduled backup did not reach the blocked upload")
			}

			configB := configA
			configB.BucketName = "portainer-archive-b"
			configB.Prefix = "portainer/next"
			configB.DailyTime = "06:00"
			_, err = service.Save(configB)
			require.NoError(t, err)
			stateAfterSaveB, err := service.readState()
			require.NoError(t, err)
			require.Equal(t, fixedTime, stateAfterSaveB.LastScheduled)
			select {
			case got := <-observedDestinations:
				require.Equal(t, configA, got)
			case <-time.After(5 * time.Second):
				cancel()
				<-done
				t.Fatal("scheduled upload destination was not observed")
			}
			close(allowUploadToFinish)

			settings := waitForLastRun(t, service, test.status, configA.BucketName)
			require.Equal(t, configB, settings.Configuration)
			require.Equal(t, &Destination{BucketName: configA.BucketName, Region: configA.Region, Prefix: configA.Prefix}, settings.LastRun.Destination)
			if test.firstErr != nil {
				require.Equal(t, test.firstErr.Error(), settings.LastRun.Message)
			}
			cancel()
			select {
			case <-done:
			case <-time.After(5 * time.Second):
				t.Fatal("scheduler did not stop after cancellation")
			}

			stateAfterRunA, err := service.readState()
			require.NoError(t, err)
			require.Equal(t, stateAfterSaveB.LastScheduled, stateAfterRunA.LastScheduled)
			require.NotEqual(t, fixedTime.Add(-15*time.Minute), stateAfterRunA.LastScheduled)

			nextRun, err := service.Execute(context.Background())
			require.NoError(t, err)
			require.Equal(t, "success", nextRun.Status)
			require.Equal(t, &Destination{BucketName: configB.BucketName, Region: configB.Region, Prefix: configB.Prefix}, nextRun.Destination)
			require.Equal(t, configB, <-observedDestinations)
		})
	}
}

func TestScheduledOrdinaryFailureConsumesDueSlot(t *testing.T) {
	t.Parallel()
	store := newServiceTestStore(t)
	service := newServiceWithArchive(t, store)
	fixedTime := time.Date(2026, time.November, 20, 4, 30, 0, 0, time.UTC)
	service.now = func() time.Time { return fixedTime }
	config := testConfiguration()
	config.DailyTime = "04:15"
	_, err := service.Save(config)
	require.NoError(t, err)
	due := fixedTime.Add(-15 * time.Minute)
	require.NoError(t, service.updateState(func(current *state) error {
		current.LastScheduled = due.Add(-24 * time.Hour)
		return nil
	}))

	var uploadCalls atomic.Int32
	service.upload = func(context.Context, Configuration, string) (string, error) {
		uploadCalls.Add(1)
		return "", errors.New("temporary S3 rejection")
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		service.schedule(ctx)
		close(done)
	}()

	settings := waitForLastRun(t, service, "failed", config.BucketName)
	require.Equal(t, "temporary S3 rejection", settings.LastRun.Message)
	current, err := service.readState()
	require.NoError(t, err)
	require.Equal(t, due, current.LastScheduled)
	time.Sleep(150 * time.Millisecond)
	require.EqualValues(t, 1, uploadCalls.Load())
	cancel()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("scheduler did not stop after cancellation")
	}
}

func TestCancelledScheduledUploadRetriesSameSlotAfterStoreReopen(t *testing.T) {
	t.Parallel()
	store := newServiceTestStore(t)
	fixedTime := time.Date(2026, time.November, 20, 4, 30, 0, 0, time.UTC)
	config := testConfiguration()
	config.DailyTime = "04:15"
	firstService := newServiceWithArchive(t, store)
	firstService.now = func() time.Time { return fixedTime }
	_, err := firstService.Save(config)
	require.NoError(t, err)
	previousSlot := fixedTime.Add(-24 * time.Hour)
	require.NoError(t, firstService.updateState(func(current *state) error {
		current.LastScheduled = previousSlot
		return nil
	}))
	firstUploadStarted := make(chan struct{})
	firstService.upload = func(ctx context.Context, _ Configuration, _ string) (string, error) {
		close(firstUploadStarted)
		<-ctx.Done()
		return "", ctx.Err()
	}

	shutdownCtx, cancelFirst := context.WithCancel(context.Background())
	firstDone := make(chan struct{})
	go func() {
		firstService.schedule(shutdownCtx)
		close(firstDone)
	}()
	select {
	case <-firstUploadStarted:
	case <-time.After(5 * time.Second):
		cancelFirst()
		<-firstDone
		t.Fatal("first scheduled upload did not start")
	}
	cancelFirst()
	select {
	case <-firstDone:
	case <-time.After(5 * time.Second):
		t.Fatal("cancelled scheduler did not stop")
	}

	afterCancellation, err := firstService.Get()
	require.NoError(t, err)
	require.NotNil(t, afterCancellation.LastRun)
	require.Equal(t, "failed", afterCancellation.LastRun.Status)
	require.Equal(t, &Destination{BucketName: config.BucketName, Region: config.Region, Prefix: config.Prefix}, afterCancellation.LastRun.Destination)
	stateAfterCancellation, err := firstService.readState()
	require.NoError(t, err)
	require.Equal(t, previousSlot, stateAfterCancellation.LastScheduled)

	require.NoError(t, store.Close())
	_, err = store.Open()
	require.NoError(t, err)

	restartedService := newServiceWithArchive(t, store)
	restartedService.now = func() time.Time { return fixedTime }
	var retryCount atomic.Int32
	retryStarted := make(chan Configuration, 2)
	restartedService.upload = func(_ context.Context, destination Configuration, _ string) (string, error) {
		retryCount.Add(1)
		retryStarted <- destination
		return "recovered-backup.tar.gz", nil
	}
	restartCtx, cancelRestart := context.WithCancel(context.Background())
	restartDone := make(chan struct{})
	go func() {
		restartedService.schedule(restartCtx)
		close(restartDone)
	}()
	select {
	case got := <-retryStarted:
		require.Equal(t, config, got)
	case <-time.After(5 * time.Second):
		cancelRestart()
		<-restartDone
		t.Fatal("fresh service did not retry the missed slot")
	}
	retried := waitForLastRun(t, restartedService, "success", config.BucketName)
	require.Equal(t, "recovered-backup.tar.gz", retried.LastRun.Key)
	require.Equal(t, &Destination{BucketName: config.BucketName, Region: config.Region, Prefix: config.Prefix}, retried.LastRun.Destination)
	currentAfterRetry, err := restartedService.readState()
	require.NoError(t, err)
	retryDue := time.Date(fixedTime.Year(), fixedTime.Month(), fixedTime.Day(), 4, 15, 0, 0, time.UTC)
	require.Equal(t, retryDue, currentAfterRetry.LastScheduled)
	time.Sleep(150 * time.Millisecond)
	require.EqualValues(t, 1, retryCount.Load())
	cancelRestart()
	select {
	case <-restartDone:
	case <-time.After(5 * time.Second):
		t.Fatal("restarted scheduler did not stop after cancellation")
	}
}

func TestScheduleStopsPendingDailyRunWhenDisabled(t *testing.T) {
	t.Parallel()

	store := newServiceTestStore(t)
	service := newServiceWithArchive(t, store)
	fixedTime := time.Date(2026, time.November, 19, 4, 14, 58, 0, time.UTC)
	_, err := service.Save(Configuration{
		BucketName: "portainer-backups",
		Region:     "ap-southeast-1",
		Enabled:    true,
		DailyTime:  "04:15",
	})
	require.NoError(t, err)
	nowCalls := make(chan struct{}, 8)
	service.now = func() time.Time {
		nowCalls <- struct{}{}
		return fixedTime
	}
	uploaded := make(chan struct{}, 1)
	service.upload = func(context.Context, Configuration, string) (string, error) {
		uploaded <- struct{}{}
		return "backup.tar.gz", nil
	}

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		service.schedule(ctx)
		close(done)
	}()
	for i := 0; i < 2; i++ {
		select {
		case <-nowCalls:
		case <-time.After(5 * time.Second):
			cancel()
			<-done
			t.Fatal("scheduler did not calculate the pending daily slot")
		}
	}
	disabled := Configuration{
		BucketName: "portainer-backups",
		Region:     "ap-southeast-1",
		DailyTime:  "04:15",
	}
	_, err = service.Save(disabled)
	require.NoError(t, err)

	select {
	case <-uploaded:
		cancel()
		<-done
		t.Fatal("scheduler ran after its daily schedule was disabled")
	case <-time.After(2500 * time.Millisecond):
	}
	cancel()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("scheduler did not stop after cancellation")
	}
}

func TestSavingScheduleInvalidatesPendingScheduledRun(t *testing.T) {
	t.Parallel()

	store := newServiceTestStore(t)
	service := newServiceWithArchive(t, store)
	_, err := service.Save(testConfiguration())
	require.NoError(t, err)
	current, err := service.readState()
	require.NoError(t, err)
	due, err := nextScheduled(current, time.Now().UTC())
	require.NoError(t, err)
	pending := &scheduleRun{due: due, revision: current.Revision}

	updated := testConfiguration()
	updated.Prefix = "new-prefix"
	_, err = service.Save(updated)
	require.NoError(t, err)
	_, err = service.execute(context.Background(), pending)
	require.ErrorIs(t, err, errScheduleChanged)
}
