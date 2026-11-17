import { useEffect, useMemo, useRef, useState } from 'react';
import CodeMirror from '@uiw/react-codemirror';
import { StreamLanguage } from '@codemirror/language';
import { json } from '@codemirror/legacy-modes/mode/javascript';

import { Button } from '@@/buttons';
import { Modal } from '@@/modals/Modal';
import { confirmGenericDiscard } from '@@/modals/confirm';
import { usePreventFormExit } from '@@/form-components/usePreventFormExit';
import { theme } from '@@/CodeEditor';
import { useCodeEditorExtensions } from '@@/CodeEditor/useCodeEditorExtensions';

import {
  editableContent,
  encodeContent,
  readTextFile,
  saveTextFile,
  TextFile,
  TextFileTarget,
  textValidationError,
} from './textFiles';

interface Props {
  target: TextFileTarget;
  onClose(): void;
  onSaved(): void;
}

export function VolumeTextEditor({ target, onClose, onSaved }: Props) {
  const [file, setFile] = useState<TextFile>();
  const [text, setText] = useState('');
  const [busy, setBusy] = useState(true);
  const [error, setError] = useState('');
  const [saved, setSaved] = useState(false);
  const busyRef = useRef(false);
  const fileName = target.path.slice(target.path.lastIndexOf('/') + 1);
  const original = useMemo(
    () =>
      file
        ? editableContent(file.Content)
        : { text: '', bom: false, crlf: false },
    [file]
  );
  const dirty = !!file && text !== original.text;
  const validation = textValidationError(fileName, text, original);
  const extensions = useCodeEditorExtensions(
    /\.ya?ml$/i.test(fileName) ? 'yaml' : undefined
  );
  const language = useMemo(
    () =>
      fileName.toLowerCase().endsWith('.json')
        ? [...extensions, StreamLanguage.define(json)]
        : extensions,
    [extensions, fileName]
  );
  usePreventFormExit(
    () => dirty || busy,
    true,
    async () => (busyRef.current ? false : confirmGenericDiscard())
  );

  useEffect(() => {
    const controller = new AbortController();
    busyRef.current = true;
    async function load() {
      try {
        const next = await readTextFile(target, controller.signal);
        const decoded = editableContent(next.Content);
        if (!controller.signal.aborted) {
          setFile(next);
          setText(decoded.text);
        }
      } catch (e: unknown) {
        if (!controller.signal.aborted) setError(message(e));
      } finally {
        if (!controller.signal.aborted) {
          setBusy(false);
          busyRef.current = false;
        }
      }
    }
    void load();
    return () => controller.abort();
    // The parent keys this dialog by environment, node, volume and file path.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);

  return (
    <Modal size="xl" aria-label={`Edit text: ${fileName}`} onDismiss={close}>
      <Modal.Header title={`Edit text — ${fileName}`} />
      <Modal.Body>
        <div className="mb-2 break-all text-sm">
          {target.volumeId}:{target.path}
        </div>
        <div className="text-muted mb-3 text-xs">
          UTF-8 · up to 1 MiB
          {file
            ? ` · ${original.crlf ? 'CRLF' : 'LF'}${original.bom ? ' · BOM preserved' : ''}`
            : ''}
          . Save updates the file directly; applications may need a reload or
          restart.
        </div>
        {error && (
          <div role="alert" className="text-danger mb-3">
            {error}
          </div>
        )}
        {file ? (
          <CodeMirror
            value={text}
            onChange={(value) => {
              setText(value);
              setSaved(false);
            }}
            readOnly={busy}
            theme={theme}
            extensions={language}
            height="400px"
            aria-label="File content"
            data-cy="volume-text-content"
            onKeyDown={(event) => {
              if (
                (event.ctrlKey || event.metaKey) &&
                event.key.toLowerCase() === 's'
              ) {
                event.preventDefault();
                void save();
              }
            }}
          />
        ) : (
          busy && <div role="status">Loading file…</div>
        )}
        {file && validation && (
          <div role="alert" className="text-danger mt-2">
            {validation}
          </div>
        )}
        {saved && (
          <div role="status" className="mt-2">
            Saved and verified.
          </div>
        )}
        {dirty && (
          <div className="text-muted mt-2 text-xs">
            Unsaved changes · Ctrl/Cmd+S to save
          </div>
        )}
      </Modal.Body>
      <Modal.Footer>
        <Button
          color="secondary"
          onClick={close}
          disabled={busy}
          data-cy="volume-text-close"
        >
          Close
        </Button>
        <Button
          color="secondary"
          onClick={reload}
          disabled={busy}
          data-cy="volume-text-reload"
        >
          Reload file
        </Button>
        <Button
          onClick={save}
          disabled={!dirty || busy || !!validation}
          data-cy="volume-text-save"
        >
          {busy && file ? 'Saving…' : 'Save'}
        </Button>
      </Modal.Footer>
    </Modal>
  );

  async function close() {
    if (busyRef.current || (dirty && !(await confirmGenericDiscard()))) return;
    onClose();
  }

  async function reload() {
    if (busyRef.current || (dirty && !(await confirmGenericDiscard()))) return;
    busyRef.current = true;
    setBusy(true);
    setError('');
    setSaved(false);
    try {
      const next = await readTextFile(target);
      const decoded = editableContent(next.Content);
      setFile(next);
      setText(decoded.text);
    } catch (e) {
      setError(message(e));
    } finally {
      busyRef.current = false;
      setBusy(false);
    }
  }

  async function save() {
    if (!file || busyRef.current || !dirty || validation) return;
    busyRef.current = true;
    setBusy(true);
    setError('');
    setSaved(false);
    try {
      const next = await saveTextFile(target, {
        Content: encodeContent(text, original),
        Version: file.Version,
      });
      const decoded = editableContent(next.Content);
      setFile(next);
      setText(decoded.text);
      setSaved(true);
      onSaved();
    } catch (e) {
      setError(message(e));
    } finally {
      busyRef.current = false;
      setBusy(false);
    }
  }
}

function message(error: unknown) {
  return error instanceof Error
    ? error.message
    : 'Unable to access the file. Your draft has been kept.';
}
