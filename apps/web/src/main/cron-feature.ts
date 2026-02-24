import { createCronDomain } from "./cron-domain.js";
import type { FeatureModule } from "./feature-contract.js";
import type { CronDomainContext } from "./types.js";

interface CronFeatureContext {
  domainContext: CronDomainContext;
}

export function createCronFeature(ctx: CronFeatureContext): FeatureModule<any> {
  const { domainContext } = ctx;
  const domain = createCronDomain(domainContext);
  let initialized = false;

  function init(): void {
    if (initialized) {
      return;
    }
    initialized = true;
    domain.bindCronEvents();
  }

  function dispose(): void {
    // cron domain currently binds listeners internally without teardown hooks.
  }

  return {
    init,
    dispose,
    actions: {
      ...domain,
    },
  };
}
