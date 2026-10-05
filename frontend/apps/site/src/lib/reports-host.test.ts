import { describe, expect, it } from 'vitest';
import { cameraHost, forCameraHost } from './reports';

describe('cameraHost', () => {
  it('names a mirror that carries camera traffic', () => {
    expect(cameraHost('openipc.ru')).toBe('openipc.ru');
  });

  it('names the origin everywhere else', () => {
    for (const host of ['openipc.org', 'dev.openipc.org', 'openipc.kz', 'localhost:4321', '']) {
      expect(cameraHost(host)).toBe('openipc.org');
    }
  });
});

describe('forCameraHost', () => {
  const fetch = '/tmp/uget openipc.org/ipctool > /tmp/ipctool\nchmod +x /tmp/ipctool\n/tmp/ipctool upload';

  it('leaves the origin as written', () => {
    expect(forCameraHost(fetch, 'openipc.org')).toBe(fetch);
  });

  it('fetches from the mirror and sends the report through it', () => {
    expect(forCameraHost(fetch, 'openipc.ru')).toBe(
      '/tmp/uget openipc.ru/ipctool > /tmp/ipctool\nchmod +x /tmp/ipctool\n/tmp/ipctool upload --host openipc.ru',
    );
  });
});
