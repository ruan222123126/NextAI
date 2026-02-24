import { createModelDomain } from "./model-domain.js";
import type { FeatureModule } from "./feature-contract.js";
import type { ModelDomainContext, WebAppState } from "./types.js";

interface ModelFeatureContext {
  domainContext: ModelDomainContext;
  state: WebAppState;
  refreshModelsButton: HTMLButtonElement;
  modelsAddProviderButton: HTMLButtonElement;
  modelsProviderForm: HTMLFormElement;
  modelsProviderHeadersAddButton: HTMLButtonElement;
  modelsProviderAliasesAddButton: HTMLButtonElement;
  modelsProviderTypeSelect: HTMLSelectElement;
  modelsProviderBaseURLInput: HTMLInputElement;
  modelsProviderCustomModelsAddButton: HTMLButtonElement;
  modelsProviderCancelButton: HTMLButtonElement;
  modelsProviderList: HTMLUListElement;
  modelsProviderHeadersRows: HTMLElement;
  modelsProviderAliasesRows: HTMLElement;
  modelsProviderCustomModelsRows: HTMLElement;
  channelsEntryList: HTMLUListElement;
  qqChannelForm: HTMLFormElement;
  qqChannelEnabledInput: HTMLInputElement;
}

export function createModelFeature(ctx: ModelFeatureContext): FeatureModule<any> {
  const {
    domainContext,
    state,
    refreshModelsButton,
    modelsAddProviderButton,
    modelsProviderForm,
    modelsProviderHeadersAddButton,
    modelsProviderAliasesAddButton,
    modelsProviderTypeSelect,
    modelsProviderBaseURLInput,
    modelsProviderCustomModelsAddButton,
    modelsProviderCancelButton,
    modelsProviderList,
    modelsProviderHeadersRows,
    modelsProviderAliasesRows,
    modelsProviderCustomModelsRows,
    channelsEntryList,
    qqChannelForm,
    qqChannelEnabledInput,
  } = ctx;

  const domain = createModelDomain(domainContext);
  const removers: Array<() => void> = [];
  let initialized = false;

  const on = (
    target: EventTarget,
    type: string,
    listener: EventListenerOrEventListenerObject,
    options?: boolean | AddEventListenerOptions,
  ): void => {
    target.addEventListener(type, listener, options);
    removers.push(() => {
      target.removeEventListener(type, listener, options);
    });
  };

  function init(): void {
    if (initialized) {
      return;
    }
    initialized = true;

    on(refreshModelsButton, "click", async () => {
      await domain.refreshModels();
    });

    on(modelsAddProviderButton, "click", () => {
      domain.openProviderModal("create");
    });

    on(modelsProviderForm, "submit", async (event: Event) => {
      event.preventDefault();
      await domain.upsertProvider();
    });

    on(modelsProviderHeadersAddButton, "click", () => {
      domain.appendProviderKVRow(modelsProviderHeadersRows, "headers");
      domain.scheduleProviderAutoSave();
    });

    on(modelsProviderAliasesAddButton, "click", () => {
      domain.appendProviderKVRow(modelsProviderAliasesRows, "aliases");
      domain.scheduleProviderAutoSave();
    });

    on(modelsProviderTypeSelect, "change", () => {
      domain.syncProviderCustomModelsField(modelsProviderTypeSelect.value);
      domain.scheduleProviderAutoSave();
    });

    on(modelsProviderBaseURLInput, "input", () => {
      domain.renderProviderBaseURLPreview();
    });

    on(modelsProviderCustomModelsAddButton, "click", () => {
      domain.appendCustomModelRow(modelsProviderCustomModelsRows);
      domain.scheduleProviderAutoSave();
    });

    on(modelsProviderForm, "input", () => {
      domain.scheduleProviderAutoSave();
    });

    on(modelsProviderForm, "change", () => {
      domain.scheduleProviderAutoSave();
    });

    on(modelsProviderForm, "click", (event: Event) => {
      const target = event.target;
      if (!(target instanceof Element)) {
        return;
      }
      const formActionButton = target.closest<HTMLButtonElement>("button[data-provider-form-action]");
      if (formActionButton) {
        const action = formActionButton.dataset.providerFormAction ?? "";
        if (action === "toggle-api-key-visibility") {
          domain.setProviderAPIKeyVisibility(!state.providerAPIKeyVisible);
        }
        if (action === "focus-base-url") {
          modelsProviderBaseURLInput.focus();
        }
        return;
      }
      const kvRemoveButton = target.closest<HTMLButtonElement>("button[data-kv-remove]");
      if (kvRemoveButton) {
        const kvRow = kvRemoveButton.closest(".kv-row");
        if (kvRow) {
          const container = kvRow.parentElement;
          kvRow.remove();
          if (container && container.children.length === 0) {
            const kind = container.getAttribute("data-kv-kind");
            if (kind === "headers" || kind === "aliases") {
              domain.appendProviderKVRow(container, kind);
            }
          }
          domain.scheduleProviderAutoSave();
        }
      }

      const customRemoveButton = target.closest<HTMLButtonElement>("button[data-custom-model-remove]");
      if (customRemoveButton) {
        const customRow = customRemoveButton.closest(".custom-model-row");
        if (!customRow) {
          return;
        }
        const customContainer = customRow.parentElement;
        customRow.remove();
        if (customContainer && customContainer.children.length === 0) {
          domain.appendCustomModelRow(customContainer);
        }
        domain.scheduleProviderAutoSave();
      }
    });

    on(modelsProviderCancelButton, "click", () => {
      domain.closeProviderModal();
    });

    on(modelsProviderList, "click", async (event: Event) => {
      const target = event.target;
      if (!(target instanceof Element)) {
        return;
      }
      const button = target.closest<HTMLButtonElement>("button[data-provider-action]");
      if (!button) {
        return;
      }
      const providerID = button.dataset.providerId ?? "";
      if (providerID === "") {
        return;
      }
      const action = button.dataset.providerAction;
      if (action === "select") {
        state.selectedProviderID = providerID;
        domain.openProviderModal("edit", providerID);
        return;
      }
      if (action === "edit") {
        domain.openProviderModal("edit", providerID);
        return;
      }
      if (action === "delete") {
        await domain.deleteProvider(providerID);
      }
    });

    on(channelsEntryList, "click", (event: Event) => {
      const target = event.target;
      if (!(target instanceof Element)) {
        return;
      }
      const button = target.closest<HTMLButtonElement>("button[data-channel-action]");
      if (!button) {
        return;
      }
      const action = button.dataset.channelAction;
      if (action !== "open") {
        return;
      }
      domain.setChannelsSettingsLevel("edit");
      domain.renderChannelsPanel();
      qqChannelEnabledInput.focus();
    });

    on(qqChannelForm, "submit", async (event: Event) => {
      event.preventDefault();
      await domain.saveQQChannelConfig();
    });
  }

  function dispose(): void {
    if (!initialized) {
      return;
    }
    initialized = false;
    while (removers.length > 0) {
      const remove = removers.pop();
      remove?.();
    }
  }

  return {
    init,
    dispose,
    actions: domain,
  };
}
