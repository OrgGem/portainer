import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { Field, Form, Formik } from 'formik';
import { Upload } from 'lucide-react';
import { boolean, object, SchemaOf, string } from 'yup';

import { notifySuccess } from '@/portainer/services/notifications';
import axios, { parseAxiosError } from '@/portainer/services/axios/axios';
import { withError } from '@/react-tools/react-query';

import { Button } from '@@/buttons/Button';
import { LoadingButton } from '@@/buttons/LoadingButton';
import { FormControl } from '@@/form-components/FormControl';
import { Input } from '@@/form-components/Input';
import { SwitchField } from '@@/form-components/SwitchField';
import { InlineLoader } from '@@/InlineLoader';

import { buildUrl } from './queries/backupSettings.service';

interface S3BackupSettings {
  bucketName: string;
  region: string;
  prefix: string;
  enabled: boolean;
  dailyTime: string;
}

interface S3BackupRun {
  at: string;
  status: 'success' | 'failed';
  message?: string;
  key?: string;
  destination?: {
    bucketName: string;
    region: string;
    prefix: string;
  };
}

interface S3BackupResponse extends S3BackupSettings {
  lastRun?: S3BackupRun;
}

const settingsKey = ['settings', 'iamS3Backup'] as const;

const validationSchema: SchemaOf<S3BackupSettings> = object({
  bucketName: string().trim().required('Bucket name is required.'),
  region: string().trim().required('Region is required.'),
  prefix: string().defined(),
  enabled: boolean().required(),
  dailyTime: string()
    .required('Daily time is required.')
    .matches(/^([01]\d|2[0-3]):[0-5]\d$/, 'Use a time in HH:mm format.'),
});

function settingsValues(settings: S3BackupResponse): S3BackupSettings {
  return {
    bucketName: settings.bucketName,
    region: settings.region,
    prefix: settings.prefix,
    enabled: settings.enabled,
    dailyTime: settings.dailyTime,
  };
}

function sameSettings(first: S3BackupSettings, second: S3BackupSettings) {
  return (
    first.bucketName === second.bucketName &&
    first.region === second.region &&
    first.prefix === second.prefix &&
    first.enabled === second.enabled &&
    first.dailyTime === second.dailyTime
  );
}

async function getSettings() {
  try {
    const { data } = await axios.get<S3BackupResponse>(
      buildUrl('s3', 'settings')
    );
    return data;
  } catch (error) {
    throw parseAxiosError(error, 'Unable to retrieve S3 backup settings');
  }
}

async function saveSettings(settings: S3BackupSettings) {
  try {
    const { data } = await axios.post<S3BackupResponse>(
      buildUrl('s3', 'settings'),
      settings
    );
    return data;
  } catch (error) {
    throw parseAxiosError(error, 'Unable to save S3 backup settings');
  }
}

async function runBackup() {
  try {
    const { data } = await axios.post<S3BackupRun>(
      buildUrl('s3', 'execute'),
      {}
    );
    return data;
  } catch (error) {
    throw parseAxiosError(error, 'Unable to back up to S3');
  }
}

export function BackupS3IAMForm() {
  const queryClient = useQueryClient();
  const settings = useQuery<S3BackupResponse, Error>(settingsKey, getSettings, {
    retry: false,
    refetchInterval: 30000,
    refetchIntervalInBackground: false,
    ...withError('Unable to retrieve S3 backup settings'),
  });
  const save = useMutation(saveSettings, {
    onMutate: () => queryClient.cancelQueries(settingsKey),
    onSuccess: (response) => {
      queryClient.setQueryData(settingsKey, response);
      notifySuccess('Success', 'S3 backup settings saved');
    },
    ...withError('Unable to save S3 backup settings'),
  });
  const backup = useMutation(runBackup, {
    onSuccess: () => notifySuccess('Success', 'Backup uploaded to S3'),
    onSettled: () => queryClient.invalidateQueries(settingsKey),
    ...withError('Unable to back up to S3'),
  });

  if (!settings.data) {
    if (settings.isInitialLoading) {
      return <InlineLoader>Loading S3 backup settings...</InlineLoader>;
    }
    return (
      <div role="alert">
        <p>Unable to load S3 backup settings.</p>
        <Button
          type="button"
          data-cy="settings-loadS3BackupRetryButton"
          onClick={() => settings.refetch()}
        >
          Retry
        </Button>
      </div>
    );
  }

  const savedValues = settingsValues(settings.data);
  const lastRun = settings.data.lastRun;

  return (
    <>
      <p className="text-muted">
        Portainer uses the IAM role available to this server to upload backups.
        The backup includes Portainer and managed stack configuration files.
      </p>
      {settings.isError && (
        <div role="alert">
          <p>Unable to refresh S3 backup settings.</p>
          <Button
            type="button"
            data-cy="settings-refreshS3BackupRetryButton"
            onClick={() => settings.refetch()}
          >
            Retry
          </Button>
        </div>
      )}
      <Formik<S3BackupSettings>
        initialValues={savedValues}
        validationSchema={validationSchema}
        validateOnMount
        onSubmit={async (values, { resetForm }) => {
          try {
            const saved = await save.mutateAsync(values);
            resetForm({ values: settingsValues(saved) });
          } catch {
            // The mutation displays the error and the form retains the changes.
          }
        }}
      >
        {({
          values,
          errors,
          touched,
          isValid,
          isSubmitting,
          setFieldValue,
          resetForm,
        }) => {
          const unsaved = !sameSettings(values, savedValues);
          const busy = isSubmitting || backup.isLoading;
          const disabled = busy || settings.isError;

          return (
            <Form className="form-horizontal">
              <FormControl
                label="Bucket name"
                inputId="iam_s3_bucket"
                errors={touched.bucketName && errors.bucketName}
                required
              >
                <Field
                  id="iam_s3_bucket"
                  name="bucketName"
                  as={Input}
                  disabled={busy}
                  data-cy="settings-backupBucketNameInput"
                />
              </FormControl>
              <FormControl
                label="Region"
                inputId="iam_s3_region"
                errors={touched.region && errors.region}
                required
              >
                <Field
                  id="iam_s3_region"
                  name="region"
                  as={Input}
                  placeholder="ap-southeast-1"
                  disabled={busy}
                  data-cy="settings-backupRegionInput"
                />
              </FormControl>
              <FormControl
                label="Object prefix"
                inputId="iam_s3_prefix"
                errors={touched.prefix && errors.prefix}
              >
                <Field
                  id="iam_s3_prefix"
                  name="prefix"
                  as={Input}
                  placeholder="portainer/backups"
                  disabled={busy}
                  data-cy="settings-backupPrefixInput"
                />
              </FormControl>
              <div className="form-group">
                <div className="col-sm-12">
                  <SwitchField
                    name="schedule-daily-backup"
                    label="Schedule daily backups"
                    labelClass="col-sm-3 col-lg-2"
                    checked={values.enabled}
                    disabled={busy}
                    onChange={(value) => setFieldValue('enabled', value)}
                    data-cy="settings-scheduleAutomaticBackupSwitch"
                  />
                </div>
              </div>
              <FormControl
                label="Daily time (UTC)"
                inputId="iam_s3_daily_time"
                errors={touched.dailyTime && errors.dailyTime}
                required
              >
                <Field
                  id="iam_s3_daily_time"
                  name="dailyTime"
                  type="time"
                  step={60}
                  as={Input}
                  disabled={busy}
                  data-cy="settings-backupDailyTimeInput"
                />
              </FormControl>
              {unsaved && (
                <p className="text-muted">
                  Save changes before running a backup.
                </p>
              )}
              {save.isError && (
                <p role="alert" className="text-danger">
                  {save.error instanceof Error
                    ? save.error.message
                    : 'Unable to save S3 backup settings.'}
                </p>
              )}
              {backup.isError && (
                <p role="alert" className="text-danger">
                  {backup.error instanceof Error
                    ? backup.error.message
                    : 'Unable to back up to S3.'}
                </p>
              )}
              <div className="form-group">
                <div className="col-sm-12 flex flex-wrap gap-2">
                  <LoadingButton
                    type="submit"
                    loadingText="Saving settings..."
                    isLoading={isSubmitting}
                    disabled={disabled || !isValid || !unsaved}
                    className="!ml-0"
                    data-cy="settings-saveBackupSettingsButton"
                  >
                    Save backup settings
                  </LoadingButton>
                  <LoadingButton
                    type="button"
                    loadingText="Uploading backup..."
                    isLoading={backup.isLoading}
                    disabled={disabled || !isValid || unsaved}
                    icon={Upload}
                    data-cy="settings-exportBackupS3Button"
                    onClick={() => backup.mutate()}
                  >
                    Backup now
                  </LoadingButton>
                  {unsaved && (
                    <Button
                      type="button"
                      color="light"
                      disabled={disabled}
                      data-cy="settings-discardS3BackupChangesButton"
                      onClick={() => resetForm({ values: savedValues })}
                    >
                      Discard changes
                    </Button>
                  )}
                </div>
              </div>
              {lastRun && (
                <div aria-live="polite" data-cy="settings-backupLastRun">
                  <span>
                    Last backup {lastRun.status} at{' '}
                    {new Date(lastRun.at).toLocaleString()}
                  </span>
                  {lastRun.destination ? (
                    <div>
                      Destination: s3://{lastRun.destination.bucketName}
                      {lastRun.destination.prefix &&
                        `/${lastRun.destination.prefix}`}{' '}
                      ({lastRun.destination.region})
                    </div>
                  ) : (
                    <div>Destination unavailable for this historical run.</div>
                  )}
                  {lastRun.status === 'success' && lastRun.key && (
                    <div>Object key: {lastRun.key}</div>
                  )}
                  {lastRun.message && <div>{lastRun.message}</div>}
                </div>
              )}
            </Form>
          );
        }}
      </Formik>
    </>
  );
}
