type DebouncedAction = () => Promise<void> | void

/**
 * Debounce rapid interaction toggles so only the latest intent hits the API.
 */
export function useDebouncedAction(delayMs = 300) {
  const timers = new Map<string, ReturnType<typeof setTimeout>>()
  const pending = new Map<string, DebouncedAction>()
  const inflight = new Map<string, Promise<void>>()

  function run(key: string, action: DebouncedAction) {
    pending.set(key, action)
    const existing = timers.get(key)
    if (existing) clearTimeout(existing)

    const timer = setTimeout(async () => {
      timers.delete(key)
      const next = pending.get(key)
      pending.delete(key)
      if (!next) return

      const waitFor = inflight.get(key)
      if (waitFor) await waitFor

      const job = Promise.resolve()
        .then(() => next())
        .catch((err) => {
          console.error('interaction failed', err)
        })
        .finally(() => {
          if (inflight.get(key) === job) inflight.delete(key)
        })
      inflight.set(key, job)
      await job
    }, delayMs)

    timers.set(key, timer)
  }

  onBeforeUnmount(() => {
    for (const t of timers.values()) clearTimeout(t)
    timers.clear()
  })

  return { run }
}
