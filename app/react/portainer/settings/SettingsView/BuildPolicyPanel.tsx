import { Cpu } from 'lucide-react';
import { Field, Form, Formik } from 'formik';
import { boolean, number, object, SchemaOf } from 'yup';

import { notifySuccess } from '@/portainer/services/notifications';

import { Widget } from '@@/Widget';
import { LoadingButton } from '@@/buttons';
import { FormControl } from '@@/form-components/FormControl';
import { SwitchField } from '@@/form-components/SwitchField';

import { useUpdateSettingsMutation } from '../queries';
import { ComposeBuildPolicy } from '../types';

export const defaultBuildPolicy: ComposeBuildPolicy = {
  CPUPercent: 50,
  MemoryPercent: 50,
  MaxConcurrentBuilds: 1,
  AutoConfigureBuilderCgroup: false,
};

export const buildPolicyValidation: SchemaOf<ComposeBuildPolicy> = object({
  AutoConfigureBuilderCgroup: boolean().required(),
  CPUPercent: number()
    .integer()
    .min(1)
    .max(50)
    .required()
    .label('CPU percentage'),
  MemoryPercent: number()
    .integer()
    .min(1)
    .max(50)
    .required()
    .label('Memory percentage'),
  MaxConcurrentBuilds: number()
    .integer()
    .min(1)
    .max(8)
    .required()
    .label('Concurrent builds'),
});

export function BuildPolicyPanel({ policy }: { policy?: ComposeBuildPolicy }) {
  const mutation = useUpdateSettingsMutation();
  return (
    <Widget id="compose-build-policy">
      <Widget.Title icon={Cpu} title="Compose build policy" />
      <Widget.Body>
        <p>
          All Compose builds managed by this Portainer server share one CPU and
          memory budget per Linux Docker host, including environments connected
          to the same host. Individual stacks cannot exceed this policy. Swap is
          disabled for the builder.
        </p>
        <p>
          Percentages use the Docker host&apos;s total logical CPUs and memory
          (the Linux VM allocation on Docker Desktop). Git checkout and Docker
          daemon overhead are outside this budget. Builds started outside
          Portainer are not managed by this policy.
        </p>
        <Formik
          initialValues={{ ...defaultBuildPolicy, ...policy }}
          enableReinitialize
          validationSchema={buildPolicyValidation}
          validateOnMount
          onSubmit={(values) =>
            mutation.mutate(
              { ComposeBuildPolicy: values },
              {
                onSuccess: () =>
                  notifySuccess('Success', 'Compose build policy saved'),
              }
            )
          }
        >
          {({ errors, touched, isValid, dirty, values, setFieldValue }) => (
            <Form className="form-horizontal">
              <FormControl
                label="CPU limit (%)"
                inputId="build-cpu-percent"
                errors={touched.CPUPercent && errors.CPUPercent}
              >
                <Field
                  id="build-cpu-percent"
                  name="CPUPercent"
                  type="number"
                  min={1}
                  max={50}
                  step={1}
                  className="form-control"
                />
              </FormControl>
              <FormControl
                label="Memory limit (%)"
                inputId="build-memory-percent"
                errors={touched.MemoryPercent && errors.MemoryPercent}
              >
                <Field
                  id="build-memory-percent"
                  name="MemoryPercent"
                  type="number"
                  min={1}
                  max={50}
                  step={1}
                  className="form-control"
                />
              </FormControl>
              <FormControl
                label="Concurrent builds per host"
                inputId="build-concurrency"
                errors={
                  touched.MaxConcurrentBuilds && errors.MaxConcurrentBuilds
                }
              >
                <Field
                  id="build-concurrency"
                  name="MaxConcurrentBuilds"
                  type="number"
                  min={1}
                  max={8}
                  step={1}
                  className="form-control"
                />
              </FormControl>
              <SwitchField
                label="Automatically configure private cgroup for builder"
                name="AutoConfigureBuilderCgroup"
                data-cy="build-private-cgroup-switch"
                checked={values.AutoConfigureBuilderCgroup}
                onChange={(value) =>
                  setFieldValue('AutoConfigureBuilderCgroup', value)
                }
              />
              <p>
                Off by default. When enabled on a cgroup v2 host, Portainer may
                recreate its idle managed builder with a private cgroup
                namespace, preserving the build cache and limits. Docker daemon
                settings and application containers are unchanged; Docker and
                the host are never restarted. Cgroup v1 hosts remain unsupported
                for managed builds.
              </p>
              <p>
                Concurrent builds share the same budget. Changes take effect
                when the host&apos;s active builds finish. Builds fail if limits
                cannot be enforced; a build that exceeds its memory limit may
                fail with an out-of-memory error. Queue wait and build time
                together are limited to one hour.
              </p>
              <LoadingButton
                type="submit"
                isLoading={mutation.isLoading}
                disabled={!isValid || !dirty}
                loadingText="Saving"
                data-cy="save-compose-build-policy"
              >
                Save build policy
              </LoadingButton>
            </Form>
          )}
        </Formik>
      </Widget.Body>
    </Widget>
  );
}
