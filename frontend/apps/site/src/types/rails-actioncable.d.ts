/**
 * `@rails/actioncable` ships no types.
 *
 * Only what this bundle calls: one consumer, one subscription, and the three
 * callbacks WallChannel actually fires. Narrow on purpose -- a fuller
 * declaration would be a second, unchecked copy of somebody else's API.
 */
declare module '@rails/actioncable' {
  export interface Subscription {
    perform(action: string, data?: Record<string, unknown>): void;
    unsubscribe(): void;
  }

  export interface Consumer {
    subscriptions: {
      create(
        identifier: Record<string, unknown>,
        handlers: {
          received?: (data: any) => void;
          connected?: () => void;
          disconnected?: () => void;
          rejected?: () => void;
        },
      ): Subscription;
    };
    disconnect(): void;
  }

  export function createConsumer(url: string): Consumer;
}
