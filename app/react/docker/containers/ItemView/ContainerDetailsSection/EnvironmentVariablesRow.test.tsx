import { render, screen } from '@testing-library/react';

import { EnvironmentVariablesRow } from './EnvironmentVariablesRow';

test('masks only the injected environment variables', () => {
  render(
    <table>
      <tbody>
        <EnvironmentVariablesRow
          variables={[
            'ORDINARY=value-from-compose',
            'DB_PASSWORD=secret-from-vault',
            'CONNECTION_URL=https://db.example/path?x=one=two',
          ]}
          maskedVariables="DB_PASSWORD"
        />
      </tbody>
    </table>
  );

  expect(screen.queryByText('secret-from-vault')).not.toBeInTheDocument();
  expect(screen.getAllByText('••••••')).toHaveLength(1);
  expect(screen.getByText('value-from-compose')).toBeVisible();
  expect(screen.getByText('https://db.example/path?x=one=two')).toBeVisible();
});

test('shows all environment values when the container has no Vault mask label', () => {
  render(
    <table>
      <tbody>
        <EnvironmentVariablesRow variables={['DB_PASSWORD=secret']} />
      </tbody>
    </table>
  );

  expect(screen.getByText('secret')).toBeVisible();
});
