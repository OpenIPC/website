/**
 * The two catalogue figures the home page states (#160, #161).
 *
 * The catalogue is data/catalogue/*.yml (#161), exported into
 * ./catalogue.json by `npm run export`, so these are a function of the same
 * tree every other page is built from and cannot disagree with it.
 *
 * Every vendor in the catalogue has SoCs; sensor makers are not in these
 * files.
 */
import { SOC_COUNT as COUNT, VENDOR_NAMES } from '../lib/hardware';

export const SOC_COUNT = COUNT;

export const SOC_VENDOR_NAMES = VENDOR_NAMES;
