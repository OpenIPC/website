import type { Meta, StoryObj } from '@storybook/preact-vite';
import ModalImage from './Modal-image';
import pic from '../../../__fixtures__/assets/webui-camera-settings.webp';

const meta: Meta<typeof ModalImage> = {
  component: ModalImage,
  title: 'Design System/Widgets/Modal Image',
};

export default meta;

export const ModalImageStory: StoryObj<typeof ModalImage> = {
  args: {
    src: pic,
    alt: 'The camera settings page of the OpenIPC WebUI',
    close: () => {},
  },
};
