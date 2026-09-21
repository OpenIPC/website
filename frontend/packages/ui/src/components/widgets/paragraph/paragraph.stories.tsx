import { Meta, StoryObj } from '@storybook/preact-vite';
import Paragraph from './paragraph';

const meta: Meta<typeof Paragraph> = {
  component: Paragraph,
  title: 'Design System/Widgets/Paragraph',
}

export default meta;

type Story = StoryObj<typeof Paragraph>;

export const ParagraphSimple: Story = {
  args: {
    content: 'If you stubmle upon an bug, please file a bug report in the appropriate repository on GitHub.',
    size: 'small',
  }
};

export const ParagraphComplex: Story = {
  args: {
    content: {
      h: 'Report issues',
      p: 'If you stubmle upon an bug, please file a bug report in the [appropriate repository on GitHub](https://github.com/OpenIPC/).',
    },
  }
};

export const ParagraphDl: Story = {
  args: {
    content: {
      h: 'Report issues',
      p: 'If you stubmle upon an bug, please file a bug report in the [appropriate repository on GitHub](https://github.com/OpenIPC/).',
      dl: true,
    },
    size: 'normal',
  }
}
