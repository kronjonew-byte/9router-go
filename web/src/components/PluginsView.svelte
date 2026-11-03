<script lang="ts">
  import { copyToClipboard } from '../lib/clipboard'
  import { api } from '../api/client'

  interface PluginItem {
    id: string
    name: string
    description: string
    version: string
    author: string
    status: 'active' | 'available'
    icon: string
  }

  const plugins: PluginItem[] = [
    {
      id: 'auto-combo-maker',
      name: 'Auto Combo Maker (AI)',
      description: 'Automatically generate and deploy intelligent model combos with failover and latency routing.',
      version: '1.0.0',
      author: '9Router Team',
      status: 'active',
      icon: 'auto_awesome',
    },
    {
      id: 'rtk-token-saver',
      name: 'RTK Token Saver',
      description: 'Auto-compress tool results, git diffs, and long prompt context to save 20-40% tokens.',
      version: '1.9.15',
      author: '9Router Team',
      status: 'active',
      icon: 'savings',
    },
    {
      id: 'prompt-guard',
      name: 'Prompt Injection Guard',
      description: 'Detect and block prompt injection attempts and PII leakage before hitting LLM upstreams.',
      version: '1.9.15',
      author: '9Router Team',
      status: 'active',
      icon: 'shield_lock',
    },
    {
      id: 'hcnsec-gateway',
      name: 'HCNSec Provider Bridge',
      description: 'Connect to HCNSec OpenAI-compatible gateway endpoints with automatic failover.',
      version: '1.1.0',
      author: 'Community',
      status: 'available',
      icon: 'hub',
    },
  ]

  let copiedMap = $state<Record<string, boolean>>({})
  let comboName = $state('auto-optimized-combo')
  let comboStrategy = $state('fallback')
  let comboKind = $state('llm')
  let isCreating = $state(false)
  let successMessage = $state('')
  let errorMessage = $state('')

  async function handleCopy(key: string, text: string) {
    const ok = await copyToClipboard(text)
    if (ok) {
      copiedMap[key] = true
      setTimeout(() => {
        copiedMap[key] = false
      }, 2000)
    }
  }

  async function handleCreateAutoCombo() {
    if (!comboName.trim()) {
      errorMessage = 'Combo name is required'
      return
    }
    isCreating = true
    errorMessage = ''
    successMessage = ''
    try {
      await api.createCombo({
        name: comboName.trim(),
        kind: comboKind,
        strategy: comboStrategy,
        models: ['gpt-4o', 'claude-3-5-sonnet'],
      })
      successMessage = `Successfully created auto combo "${comboName.trim()}" with models! Check the Combos tab.`
      comboName = 'auto-optimized-combo-' + Math.floor(Math.random() * 1000)
    } catch (e: any) {
      errorMessage = e?.message || 'Failed to create combo'
    } finally {
      isCreating = false
    }
  }
</script>

<div class="max-w-4xl mx-auto space-y-6">
  <!-- Header Banner -->
  <div class="p-5 rounded-[14px] bg-surface border border-border-subtle shadow-[var(--shadow-soft)]">
    <div class="flex items-center justify-between flex-wrap gap-4">
      <div>
        <h2 class="text-base font-semibold text-text-main">Gateway Plugins & Extensions</h2>
        <p class="text-xs text-text-muted mt-1">
          Extend 9router-go capabilities with built-in modules and community extensions.
        </p>
      </div>
      <div class="px-3 py-1.5 rounded-lg bg-brand-500/10 border border-brand-500/30 text-brand-600 dark:text-brand-400 text-xs font-medium">
        4 Plugins Installed
      </div>
    </div>
  </div>

  <!-- Auto Combo Maker Interactive Plugin Section -->
  <div class="p-5 rounded-[14px] bg-surface border border-border-subtle shadow-[var(--shadow-soft)] space-y-4">
    <div class="flex items-center gap-3">
      <div class="size-10 rounded-xl flex items-center justify-center bg-brand-500/10 text-brand-600 dark:text-brand-400">
        <span class="material-symbols-outlined text-[20px]">auto_awesome</span>
      </div>
      <div>
        <h3 class="font-semibold text-sm text-text-main">Auto Combo Maker Assistant</h3>
        <p class="text-xs text-text-muted">Configure and auto-generate intelligent model routing combos instantly.</p>
      </div>
    </div>

    <div class="grid grid-cols-1 sm:grid-cols-3 gap-3 pt-2">
      <div class="space-y-1">
        <label for="combo-name-input" class="text-xs font-medium text-text-muted">Combo Name</label>
        <input
          id="combo-name-input"
          type="text"
          bind:value={comboName}
          class="w-full px-3 py-2 rounded-lg bg-surface-2 border border-border-subtle text-text-main text-xs focus:outline-none focus:border-brand-500"
          placeholder="e.g. fast-fallback-combo"
        />
      </div>
      <div class="space-y-1">
        <label for="combo-strategy-select" class="text-xs font-medium text-text-muted">Strategy</label>
        <select
          id="combo-strategy-select"
          bind:value={comboStrategy}
          class="w-full px-3 py-2 rounded-lg bg-surface-2 border border-border-subtle text-text-main text-xs focus:outline-none focus:border-brand-500"
        >
          <option value="inherit">Inherit global strategy</option>
          <option value="fallback">Fallback — try in order</option>
          <option value="round-robin">Round Robin — rotate</option>
          <option value="sticky">Sticky — stay until the limit</option>
          <option value="capacity">Capacity — prefer free tiers</option>
          <option value="smart">Smart Router — System One</option>
          <option value="fusion">Fusion — panel + judge</option>
          <option value="tokenmaxxing">Tokenmaxxing — quota-aware</option>
        </select>
      </div>
      <div class="space-y-1">
        <label for="combo-kind-select" class="text-xs font-medium text-text-muted">Kind</label>
        <select
          id="combo-kind-select"
          bind:value={comboKind}
          class="w-full px-3 py-2 rounded-lg bg-surface-2 border border-border-subtle text-text-main text-xs focus:outline-none focus:border-brand-500"
        >
          <option value="chat">Chat / LLM</option>
          <option value="vision">Vision</option>
          <option value="audio">Audio / TTS</option>
        </select>
      </div>
    </div>

    {#if successMessage}
      <div class="p-3 rounded-lg bg-success/10 border border-success/30 text-success text-xs font-medium">
        {successMessage}
      </div>
    {/if}

    {#if errorMessage}
      <div class="p-3 rounded-lg bg-error/10 border border-error/30 text-error text-xs font-medium">
        {errorMessage}
      </div>
    {/if}

    <div class="flex justify-end pt-1">
      <button
        type="button"
        onclick={handleCreateAutoCombo}
        disabled={isCreating}
        class="px-4 py-2 rounded-lg bg-brand-500 hover:bg-brand-600 text-white text-xs font-medium transition-colors cursor-pointer disabled:opacity-50 inline-flex items-center gap-2 shadow-sm"
      >
        <span class="material-symbols-outlined text-[16px]">bolt</span>
        <span>{isCreating ? 'Creating Combo...' : 'Generate & Create Combo'}</span>
      </button>
    </div>
  </div>

  <!-- Plugin Cards List -->
  <div class="space-y-3">
    {#each plugins as plugin (plugin.id)}
      {@const isCopied = !!copiedMap[plugin.id]}
      <div
        class="flex items-start gap-3.5 p-4 rounded-[14px] border border-border-subtle bg-surface hover:bg-surface-2 shadow-[var(--shadow-soft)] transition-colors"
      >
        <!-- Icon box -->
        <div
          class="size-10 rounded-xl flex items-center justify-center shrink-0 bg-brand-500/10 text-brand-600 dark:text-brand-400"
        >
          <span class="material-symbols-outlined text-[20px]">{plugin.icon}</span>
        </div>

        <!-- Details -->
        <div class="min-w-0 flex-1">
          <div class="flex items-center gap-2 flex-wrap">
            <h3 class="font-semibold text-sm text-text-main">{plugin.name}</h3>
            <span
              class="px-2 py-0.5 rounded-full text-[10px] font-bold uppercase tracking-wider {plugin.status === 'active'
                ? 'bg-success/15 text-success border border-success/30'
                : 'bg-surface-3 text-text-muted border border-border-subtle'}"
            >
              {plugin.status}
            </span>
            <span class="text-[11px] font-mono text-text-muted">v{plugin.version}</span>
            <span class="text-[11px] text-text-muted">by {plugin.author}</span>
          </div>

          <p class="text-xs text-text-muted mt-1 leading-relaxed">{plugin.description}</p>
        </div>

        <!-- Action button -->
        <button
          type="button"
          onclick={() => handleCopy(plugin.id, plugin.id)}
          class="px-2.5 py-1.5 rounded-md bg-surface-2 hover:bg-surface-3 border border-border-subtle text-text-main text-[11px] font-medium transition-colors cursor-pointer shrink-0 inline-flex items-center gap-1.5"
        >
          <span class="material-symbols-outlined text-[13px]">
            {isCopied ? 'check' : 'content_copy'}
          </span>
          <span>{isCopied ? 'Copied ID' : 'Copy ID'}</span>
        </button>
      </div>
    {/each}
  </div>
</div>
