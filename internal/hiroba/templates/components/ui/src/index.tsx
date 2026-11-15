import type { ComponentType } from 'react';
import { ClusterTabBlueprint, createExtension } from '@7k-inari/ui-plugin-sdk';
import type { ClusterTabSlotProps } from '@7k-inari/ui-plugin-sdk';

const Health: ComponentType<ClusterTabSlotProps> = () => (
  <section data-inari-slot="health" aria-label="Scaffold health">Synthetic extension healthy</section>
);

export default createExtension({
  manifest: {
    name: '__HIROBA_NAME__',
    version: '0.1.0',
    kind: 'ui',
    title: 'Workflows scaffold',
    description: 'CI-only public SDK fixture',
  },
  slots: [ClusterTabBlueprint({ name: 'health', title: 'Health', component: Health })],
});
