import axios, { parseAxiosError } from '@/portainer/services/axios/axios';
import { withAgentTargetHeader } from '@/react/docker/proxy/queries/utils';

export const MAX_TEXT_BYTES = 1024 * 1024;
export interface TextFile {
  Content: string;
  Version: string;
}
export interface TextFileTarget {
  endpointId: number;
  volumeId: string;
  path: string;
  nodeName?: string;
}

export async function readTextFile(
  target: TextFileTarget,
  signal?: AbortSignal
) {
  try {
    const { data } = await axios.get<TextFile>(url(target), {
      params: params(target),
      headers: withAgentTargetHeader(target.nodeName),
      signal,
      timeout: 35000,
    });
    return data;
  } catch (e) {
    throw parseAxiosError(e, 'Unable to open text file');
  }
}
export async function saveTextFile(target: TextFileTarget, file: TextFile) {
  try {
    const { data } = await axios.put<TextFile>(url(target), file, {
      params: params(target),
      headers: withAgentTargetHeader(target.nodeName),
      timeout: 35000,
    });
    return data;
  } catch (e) {
    throw parseAxiosError(e, 'Unable to save text file');
  }
}
function url(target: TextFileTarget) {
  return `/endpoints/${target.endpointId}/docker/v2/browse/text`;
}
function params(target: TextFileTarget) {
  return { volumeID: target.volumeId, path: target.path };
}

export function editableContent(content: string) {
  const bom = content.startsWith('\uFEFF');
  const raw = bom ? content.slice(1) : content;
  const crlf = raw.includes('\r\n');
  if (
    raw.replace(/\r\n/g, '').includes('\r') ||
    (crlf && raw.replace(/\r\n/g, '').includes('\n'))
  ) {
    throw new Error(
      'Mixed or CR-only line endings are not supported by quick edit. Use download/upload for this file.'
    );
  }
  return { text: raw.replace(/\r\n/g, '\n'), bom, crlf };
}

export function encodeContent(
  text: string,
  original: ReturnType<typeof editableContent>
) {
  return `${original.bom ? '\uFEFF' : ''}${original.crlf ? text.replace(/\n/g, '\r\n') : text}`;
}

export function textValidationError(
  fileName: string,
  text: string,
  original: ReturnType<typeof editableContent>
) {
  if (
    new TextEncoder().encode(encodeContent(text, original)).length >
    MAX_TEXT_BYTES
  )
    return 'Quick edit supports files up to 1 MiB.';
  if (fileName.toLowerCase().endsWith('.json')) {
    try {
      JSON.parse(text);
    } catch {
      return 'Invalid JSON. Correct the document before saving.';
    }
  }
  return '';
}
