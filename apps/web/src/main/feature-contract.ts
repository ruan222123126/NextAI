export interface FeatureModule<TActions> {
  init(): void;
  dispose(): void;
  actions: TActions;
}
