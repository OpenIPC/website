import type { Meta, StoryObj } from '@storybook/preact-vite';
import PartitionMap from './Part-map';

const meta = {
  component: PartitionMap,
  title: 'Design System/Widgets/Partition Map',
  argTypes: {
    freeSpace: {
      options: ['10', '100', '1000', '10000'],
      control: 'select',
    },
    slices: {
      options: ['Empty', 'One slice', 'Two slices', 'Eight slices'],
      control: 'radio',
      mapping: {
        'Empty': [],
        'One slice': [
          {
            width: 20,
            color: 'partition0',
          },
        ],
        'Two slices': [
          {
            width: 20,
            color: 'partition0',
          },
          {
            width: 60,
            color: 'partition1',
          },
        ],
        'Eight slices': [
          {
            width: 10,
            color: 'partition0',
          },
          {
            width: 15,
            color: 'partition1',
          },
          {
            width: 10,
            color: 'partition2',
          },
          {
            width: 12,
            color: 'partition3',
          },
          {
            width: 10,
            color: 'partition4',
          },
          {
            width: 10,
            color: 'partition5',
          },
          {
            width: 10,
            color: 'partition6',
          },
          {
            width: 10,
            color: 'partition7',
          },
        ],
      },
    },
  },
} satisfies Meta<typeof PartitionMap>;

export default meta;

type Story = StoryObj<typeof PartitionMap>;

export const PartitionMapStory: Story = {
  args: {
    slices: [
      {
        width: 20,
        color: 'partition0',
      },
      {
        width: 60,
        color: 'partition1',
      },
    ],
  }
}
