import YAML from 'yaml';

export function composeServices(content: string): string[] {
  try {
    const document = YAML.parse(content, { maxAliasCount: 10000 });
    if (
      !document ||
      typeof document !== 'object' ||
      !document.services ||
      typeof document.services !== 'object' ||
      Array.isArray(document.services)
    ) {
      return [];
    }
    return Object.keys(document.services).sort();
  } catch {
    return [];
  }
}

export function readComposeFile(file: File): Promise<string> {
  if (typeof file.text === 'function') {
    return file.text();
  }

  return new Promise((resolve, reject) => {
    const reader = new FileReader();
    reader.onload = () =>
      resolve(typeof reader.result === 'string' ? reader.result : '');
    reader.onerror = () => reject(new Error('Unable to read Compose file'));
    reader.readAsText(file);
  });
}
