import { useMutation, useQueryClient } from '@tanstack/react-query';

import axios, { parseAxiosError } from '@/portainer/services/axios/axios';
import {
  mutationOptions,
  withError,
  withInvalidate,
} from '@/react-tools/react-query';

export function useUpdateSSLConfigMutation() {
  const queryClient = useQueryClient();

  return useMutation(
    updateSSLConfig,
    mutationOptions(
      withError('Unable to update SSL configuration'),
      withInvalidate(queryClient, [['sslSettings']])
    )
  );
}

interface SSLConfig {
  // SSL Certificates
  certFile?: File;
  keyFile?: File;
  httpEnabled?: boolean;

  // SSL Client Certificates
  clientCertFile?: File;
}

async function updateSSLConfig({
  certFile,
  keyFile,
  clientCertFile,
  ...payload
}: SSLConfig) {
  try {
    const cert = certFile ? await certFile.text() : undefined;
    const key = keyFile ? await keyFile.text() : undefined;
    const clientCert = clientCertFile ? await clientCertFile.text() : undefined;

    await axios.put('/ssl', {
      ...payload,
      cert,
      key,
      clientCert,
    });
  } catch (error) {
    throw parseAxiosError(error, 'Unable to update SSL configuration');
  }
}
