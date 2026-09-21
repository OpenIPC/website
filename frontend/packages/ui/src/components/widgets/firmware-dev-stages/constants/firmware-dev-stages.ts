import { SoCItem } from '../../soc-managed-list/types';

type FirmwareDevStage = {
  stage: SoCItem['stage'],
  h: string,
  p: string,
}

export const firmwareDevStagesConstantsH2 = 'Stages of Firmware development';

export const firmwareDevStagesConstants:FirmwareDevStage[] = [
  {
    stage: 'NEQ',
    h: 'NEQ',
    p: 'No Equipment. We have an SDK for the platform, but we don\'t have specific hardware to continue development, you can donate it to our R&D guys (especially old boards that passed EOL and can not longer be bought).',
  },
  {
    stage: 'RND',
    h: 'R&D',
    p: 'The Research and Development stage, when we already have the platform SDK and maybe even the hardware boards, but we are starting to fiddle with the platform, studying its specifics and features. There is still a lot of work ahead.',
  },
  {
    stage: 'HLP',
    h: 'HLP',
    p: 'Help wanted. We have the hardware, we have the SDK, the basic stuff is learned and done. But we are stuck. This is where we are looking for help from experienced embedded developers to overcome the obstacles and move on to the next stage.',
  },
  {
    stage: 'WIP',
    h: 'WIP',
    p: 'Work in progress. We learned a lot about the platform hardware and code base, prepared the first public binary build, and are waiting for the first adopters to test it on their boards and provide feedback to help us move forward.',
  },
  {
    stage: 'MVP',
    h: 'MVP',
    p: 'A minimal viable product. The basic system is built, the platform can produce video, at least on the main channel, but due to a lack of human resources, development is delayed or halted. A financial infusion could push the development to the final stage.',
  },
  {
    stage: 'DONE',
    h: 'DONE',
    p: 'Done and done! Bootloader boots, Linux loads, streamer can stream video and produce snaphots. You can have yourself a working platform open for tinkering and further improvements. We still expect feedback and patches from you guys, though.',
  },
];
