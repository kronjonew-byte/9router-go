<script lang="ts">
  import { onMount } from 'svelte'
  import { api, type CustomPluginConfigMap } from '../api/client'

  interface CustomPluginItem {
    id: string
    name: string
    description: string
    icon: string
    hasLevel?: boolean
  }

  const customPluginsMeta: CustomPluginItem[] = [
    {
      id: 'imageVision',
      name: 'Image Vision',
      description: 'Pass raw image blocks through to the translator/model and nudge the model to describe what it sees.',
      icon: 'image',
    },
    {
      id: 'thinkDeeper',
      name: 'Think Deeper',
      description: 'Set native reasoning effort high / maximal thinking budget so the provider routes to its deepest tier.',
      icon: 'psychology',
    },
    {
      id: 'speedMode',
      name: 'Speed Mode',
      description: 'Disable reasoning/thinking (effort: none) so responses come back instantly with no chain-of-thought overhead.',
      icon: 'bolt',
    },
    {
      id: 'jsonGuard',
      name: 'JSON Guard',
      description: 'Auto-set response_format from prose hints; repairs fenced/messy JSON output back to machine-readable form.',
      icon: 'data_object',
    },
    {
      id: 'contextSqueezer',
      name: 'Context Squeezer',
      description: 'Trim conversations that exceed a model context window, keeping newest turns behind a recap.',
      icon: 'compress',
    },
    {
      id: 'openaiToolBridge',
      name: 'OpenAI Tool Bridge',
      description: 'Bridge legacy functions declarations into the native tools schema where a provider requires it.',
      icon: 'handyman',
    },
    {
      id: 'antiSlop',
      name: 'Anti-Slop',
      description: 'Strip repetitive AI filler phrases from outgoing text. Level: lite (minimal) or full (aggressive).',
      icon: 'format_quote',
      hasLevel: true,
    },
  ]

  let customPlugins = $state<CustomPluginConfigMap>({})
  let pluginsLoading = $state(true)
  let pluginsSaving = $state(false)
  let pluginsSaved = $state('')
  let pluginsError = $state('')

  // Number of custom plugins currently enabled, derived from the live config.
  const enabledCount = $derived(customPluginsMeta.filter((m) => !!customPlugins[m.id]?.enabled).length)

  async function loadPlugins() {
    pluginsLoading = true
    pluginsError = ''
    try {
      const cfg = await api.getCustomPlugins()
      // merge over known keys so toggles always render
      const merged: CustomPluginConfigMap = {}
      for (const meta of customPluginsMeta) {
        merged[meta.id] = {
          enabled: !!cfg[meta.id]?.enabled,
          models: cfg[meta.id]?.models || [],
          level: (cfg[meta.id]?.level as 'lite' | 'full') || (meta.hasLevel ? 'full' : undefined),
        }
      }
      customPlugins = merged
    } catch (e: any) {
      pluginsError = e?.message || 'Failed to load plugins'
    } finally {
      pluginsLoading = false
    }
  }

  async function savePlugins() {
    pluginsSaving = true
    pluginsSaved = ''
    pluginsError = ''
    try {
      await api.putCustomPlugins(customPlugins)
      pluginsSaved = 'Custom plugin config saved.'
    } catch (e: any) {
      pluginsError = e?.message || 'Failed to save plugins'
    } finally {
      pluginsSaving = false
    }
  }

  function modelsText(id: string): string {
    return (customPlugins[id]?.models || []).join(', ')
  }

  function setModelsText(id: string, value: string) {
    const parts = value.split(',').map((s) => s.trim()).filter(Boolean)
    const p = customPlugins[id]
    if (p) p.models = parts
  }

  onMount(() => {
    loadPlugins()
  })
</script>


<div class="max-w-4xl mx-auto space-y-6">
  <!-- Header Banner -->
  <div class="p-5 rounded-[14px] bg-surface border border-border-subtle shadow-[var(--shadow-soft)]">
    <div class="flex items-center justify-between flex-wrap gap-4">
      <div>
        <h2 class="text-base font-semibold text-text-main">Gateway Plugins</h2>
        <p class="text-xs text-text-muted mt-1">
          Request-transformer plugins that run inside the 9router-go proxy. Enable one, scope it to
          specific models, then save.
        </p>
      </div>
      <div class="px-3 py-1.5 rounded-lg bg-brand-500/10 border border-brand-500/30 text-brand-600 dark:text-brand-400 text-xs font-medium">
        {enabledCount} of {customPluginsMeta.length} Active
      </div>
    </div>
  </div>

  <!-- Custom Plugins (Go request transformers) Section -->
  <div class="p-5 rounded-[14px] bg-surface border border-border-subtle shadow-[var(--shadow-soft)] space-y-4">
    <div class="flex items-center justify-between flex-wrap gap-3">
      <div class="flex items-center gap-3">
        <div class="size-10 rounded-xl flex items-center justify-center bg-brand-500/10 text-brand-600 dark:text-brand-400">
          <span class="material-symbols-outlined text-[20px]">extension</span>
        </div>
        <div>
          <h3 class="font-semibold text-sm text-text-main">Custom Plugins (Go Runtime)</h3>
          <p class="text-xs text-text-muted">Request transformers that run inside the Go proxy: vision passthrough, thinking depth, JSON guard, context squeezer, tool bridge and anti-slop.</p>
        </div>
      </div>
      <div class="flex items-center gap-2">
        {#if pluginsLoading}
          <span class="text-xs text-text-muted">Loading…</span>
        {:else}
          <button
            type="button"
            onclick={savePlugins}
            disabled={pluginsSaving}
            class="px-4 py-2 rounded-lg bg-brand-500 hover:bg-brand-600 text-white text-xs font-medium transition-colors cursor-pointer disabled:opacity-50 inline-flex items-center gap-2 shadow-sm"
          >
            <span class="material-symbols-outlined text-[16px]">save</span>
            <span>{pluginsSaving ? 'Saving…' : 'Save Plugins'}</span>
          </button>
        {/if}
      </div>
    </div>

    {#if pluginsSaved}
      <div class="p-3 rounded-lg bg-success/10 border border-success/30 text-success text-xs font-medium">
        {pluginsSaved}
      </div>
    {/if}
    {#if pluginsError}
      <div class="p-3 rounded-lg bg-error/10 border border-error/30 text-error text-xs font-medium">
        {pluginsError}
      </div>
    {/if}

    {#if pluginsLoading}
      <div class="text-center py-6 text-xs text-text-muted">Loading custom plugin configuration…</div>
    {:else}
      <div class="space-y-3">
        {#each customPluginsMeta as meta (meta.id)}
          {@const plugin = customPlugins[meta.id]}
          <div
            class="flex items-start gap-3.5 p-4 rounded-[14px] border border-border-subtle bg-surface-2 hover:bg-surface transition-colors"
          >
            <div
              class="size-10 rounded-xl flex items-center justify-center shrink-0 bg-brand-500/10 text-brand-600 dark:text-brand-400"
            >
              <span class="material-symbols-outlined text-[20px]">{meta.icon}</span>
            </div>
            <div class="min-w-0 flex-1">
              <div class="flex items-center gap-2 flex-wrap">
                <h4 class="font-semibold text-sm text-text-main">{meta.name}</h4>
                <span
                  class="px-2 py-0.5 rounded-full text-[10px] font-bold uppercase tracking-wider {plugin?.enabled
                    ? 'bg-success/15 text-success border border-success/30'
                    : 'bg-surface-3 text-text-muted border border-border-subtle'}"
                >
                  {plugin?.enabled ? 'enabled' : 'disabled'}
                </span>
              </div>
              <p class="text-xs text-text-muted mt-1 leading-relaxed">{meta.description}</p>

              {#if meta.hasLevel}
                <div class="flex items-center gap-2 mt-3">
                  <label class="text-[11px] font-medium text-text-muted" for={`${meta.id}-level`}>Level</label>
                  <select
                    id={`${meta.id}-level`}
                    bind:value={plugin.level}
                    class="px-2 py-1 rounded-md bg-surface-3 border border-border-subtle text-text-main text-[11px] focus:outline-none focus:border-brand-500"
                  >
                    <option value="lite">Lite</option>
                    <option value="full">Full</option>
                  </select>
                </div>
              {/if}

              <div class="mt-2">
                <label class="text-[11px] font-medium text-text-muted" for={`${meta.id}-models`}>
                  Models (comma-separated; empty = all)
                </label>
                <input
                  id={`${meta.id}-models`}
                  type="text"
                  value={modelsText(meta.id)}
                  oninput={(e) => setModelsText(meta.id, e.currentTarget.value)}
                  placeholder="e.g. claude-sonnet-4-6, gpt-4o"
                  class="w-full px-2.5 py-1.5 rounded-md bg-surface-3 border border-border-subtle text-text-main text-[11px] focus:outline-none focus:border-brand-500 mt-1"
                />
              </div>
            </div>
            <!-- Toggle -->
            <button
              type="button"
              onclick={() => (plugin.enabled = !plugin.enabled)}
              class="relative inline-flex h-6 w-11 shrink-0 items-center rounded-full transition-colors cursor-pointer {plugin?.enabled
                ? 'bg-brand-500'
                : 'bg-surface-3 border border-border-subtle'}"
              aria-label={`Toggle ${meta.name}`}
            >
              <span
                class="inline-block size-4 transform rounded-full bg-white transition-transform {plugin?.enabled
                  ? 'translate-x-[24px]'
                  : 'translate-x-[3px]'}"
              ></span>
            </button>
          </div>
        {/each}
      </div>
    {/if}
  </div>
</div>
