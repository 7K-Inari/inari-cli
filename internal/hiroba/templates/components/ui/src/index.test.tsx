import { describe, expect, it } from 'vitest';
import extension from './index';

describe('public UI extension', () => {
  it('declares the reviewed cluster-tab slot', () => {
    expect(extension.manifest.name).toBe('__HIROBA_NAME__');
    expect(extension.manifest.slots).toEqual([{ kind: 'cluster-tab', name: 'health' }]);
    expect(extension.slots[0].component).toBeTypeOf('function');
  });
});
