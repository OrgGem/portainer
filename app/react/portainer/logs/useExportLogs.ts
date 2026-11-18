import { useMutation } from '@tanstack/react-query';
import { isAxiosError, isCancel } from 'axios';
import { saveAs } from 'file-saver';
import { useEffect, useRef } from 'react';

import axios, { parseAxiosError } from '@/portainer/services/axios/axios';

import { LogQuery } from './types';
import { LogEndpoint } from './useLogsQuery';

export function useExportLogs(endpoint: LogEndpoint) {
  const controllers = useRef(new Set<AbortController>());

  useEffect(() => {
    const activeControllers = controllers.current;
    return () => {
      activeControllers.forEach((controller) => controller.abort());
      activeControllers.clear();
    };
  }, []);

  return useMutation({
    mutationFn: async (query: LogQuery) => {
      const controller = new AbortController();
      controllers.current.add(controller);
      // CSV always includes all matching rows, regardless of the table page.
      const filters = { ...query };
      delete filters.offset;
      delete filters.limit;
      try {
        const { data, headers } = await axios.get<Blob>(
          `/useractivity/${endpoint}.csv`,
          { params: filters, responseType: 'blob', signal: controller.signal }
        );
        if (!controller.signal.aborted) {
          saveAs(
            data,
            filenameFromHeader(
              headers['content-disposition'],
              `${endpoint}.csv`
            )
          );
        }
      } catch (error) {
        if (controller.signal.aborted || isCancel(error)) {
          return;
        }
        const message = await getBlobErrorMessage(error);
        if (controller.signal.aborted) {
          return;
        }
        if (message) {
          throw new Error(`Failed to export logs: ${message}`, {
            cause: error,
          });
        }
        throw parseAxiosError(error, 'Failed to export logs');
      } finally {
        controllers.current.delete(controller);
      }
    },
  });
}

export function filenameFromHeader(header: unknown, fallback: string) {
  if (typeof header !== 'string') {
    return fallback;
  }

  const encoded = /(?:^|;)\s*filename\*\s*=\s*UTF-8''([^;]+)/i.exec(
    header
  )?.[1];
  const plain = /(?:^|;)\s*filename\s*=\s*(?:"([^"]*)"|([^;]*))/i.exec(header);
  let filename = plain?.[1] || plain?.[2] || '';
  if (encoded) {
    try {
      filename = decodeURIComponent(encoded.trim());
    } catch {
      // A malformed extended filename can still have a valid plain fallback.
    }
  }
  filename = [...filename.trim().replace(/[\\/<>:"|?*]/g, '_')]
    .map((character) => {
      const code = character.charCodeAt(0);
      return code < 32 || code === 127 ? '_' : character;
    })
    .join('');
  return filename && filename !== '.' && filename !== '..'
    ? filename
    : fallback;
}

async function getBlobErrorMessage(error: unknown) {
  if (!isAxiosError(error) || !(error.response?.data instanceof Blob)) {
    return undefined;
  }

  try {
    const body = (await error.response.data.slice(0, 4096).text()).trim();
    try {
      const parsed: unknown = JSON.parse(body);
      if (parsed && typeof parsed === 'object') {
        const fields = parsed as Record<string, unknown>;
        const message = fields.details || fields.message || fields.error;
        return typeof message === 'string' ? message : undefined;
      }
      return typeof parsed === 'string' ? parsed : undefined;
    } catch {
      return body || undefined;
    }
  } catch {
    return undefined;
  }
}
