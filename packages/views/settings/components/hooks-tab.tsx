"use client";

import { useMemo } from "react";
import { useQuery } from "@tanstack/react-query";
import { CircleSlash, LoaderCircle, Webhook } from "lucide-react";
import { useWorkspaceId } from "@multica/core/hooks";
import {
  providerDisplayName,
  runtimeHookListOptions,
} from "@multica/core/runtimes";
import type { RuntimeHook, RuntimeHookGroup } from "@multica/core/types";
import { Badge } from "@multica/ui/components/ui/badge";
import { cn } from "@multica/ui/lib/utils";
import { useT } from "../../i18n";
import { SettingsCard, SettingsTab } from "./settings-layout";

/**
 * Read-only hook inventory (COC-341). It answers one question that used to
 * require SSH: is the Multica hook actually installed on that machine, and has
 * anyone ever seen it run.
 *
 * Two collapses this surface must never make:
 *
 *   - `never_fired` vs `unobserved`. The first is an observation, the second is
 *     the absence of one. Showing a hook installed before telemetry existed as
 *     "never fired" accuses a working hook of being dead.
 *   - an unsupported provider vs an empty list. An empty list reads as
 *     "supported, and you installed none", which sends someone off to debug an
 *     installation that was never possible.
 */

type TelemetryTone = "fired" | "quiet" | "unknown";

// Telemetry states this build knows how to phrase. A state a newer backend
// invents falls through to the default branch and renders as itself.
const TELEMETRY_TONES: Record<string, TelemetryTone> = {
  fired: "fired",
  never_fired: "quiet",
  unobserved: "unknown",
  uncollectable: "unknown",
};

function telemetryToneClass(telemetry: string): string {
  switch (TELEMETRY_TONES[telemetry]) {
    case "fired":
      return "text-success";
    case "quiet":
      return "text-warning";
    case "unknown":
      return "text-muted-foreground";
    default:
      return "text-muted-foreground";
  }
}

function formatTimestamp(value: string | null): string {
  if (!value) return "";
  const parsed = new Date(value);
  if (Number.isNaN(parsed.getTime())) return "";
  return parsed.toLocaleString();
}

function TelemetryLabel({ hook }: { hook: RuntimeHook }) {
  const { t } = useT("settings");
  const firedAt = formatTimestamp(hook.last_fired_at);

  // Four states, four sentences. Collapsing any pair here is the bug this
  // whole inventory exists to avoid.
  let text: string;
  switch (hook.telemetry) {
    case "fired":
      text = firedAt
        ? t(($) => $.hooks.telemetry.fired_at, { when: firedAt })
        : t(($) => $.hooks.telemetry.fired);
      break;
    case "never_fired":
      text = t(($) => $.hooks.telemetry.never_fired);
      break;
    case "unobserved":
      text = t(($) => $.hooks.telemetry.unobserved);
      break;
    case "uncollectable":
      text = t(($) => $.hooks.telemetry.uncollectable);
      break;
    default:
      // A state this build has not heard of renders as itself rather than
      // being silently folded into one of the four above.
      text = hook.telemetry;
      break;
  }

  return (
    <span
      className={cn(
        "text-caption tabular-nums",
        telemetryToneClass(hook.telemetry),
      )}
    >
      {text}
    </span>
  );
}

function HookRow({ hook }: { hook: RuntimeHook }) {
  const { t } = useT("settings");

  return (
    <div className="flex flex-col gap-2 px-4 py-3.5 sm:flex-row sm:items-start sm:justify-between sm:gap-6">
      <div className="min-w-0 flex-1 space-y-1">
        <div className="flex flex-wrap items-center gap-2">
          <span className="truncate text-body font-medium">{hook.hook_name}</span>
          {hook.event ? (
            <Badge variant="outline" className="font-normal">
              {hook.event}
            </Badge>
          ) : null}
          {hook.enabled === true ? null : (
            <Badge variant="secondary" className="font-normal">
              {t(($) => $.hooks.hook.disabled)}
            </Badge>
          )}
        </div>
        {hook.trigger_spec ? (
          <div className="text-caption text-muted-foreground">
            <span>{t(($) => $.hooks.hook.trigger)}</span>{" "}
            <code className="break-all font-mono">{hook.trigger_spec}</code>
          </div>
        ) : null}
        {hook.command_path ? (
          <div className="text-caption text-muted-foreground">
            <span>{t(($) => $.hooks.hook.command)}</span>{" "}
            <code className="break-all font-mono">{hook.command_path}</code>
          </div>
        ) : null}
      </div>
      <div className="shrink-0 sm:text-right">
        <TelemetryLabel hook={hook} />
      </div>
    </div>
  );
}

function RuntimeGroup({ group }: { group: RuntimeHookGroup }) {
  const { t } = useT("settings");

  const online = group.status === "online";
  const lastSeen = formatTimestamp(group.last_seen_at);
  const observedAt = formatTimestamp(group.observed_at);

  // Three distinct empty answers, never one. `supported === false` wins over
  // everything else: the provider has no place to put a hook, so "none
  // installed" would be a wrong instruction, not a shorter one.
  const unsupported = group.supported === false;
  const neverScanned = !unsupported && group.observed_at === null;
  const noHooks =
    !unsupported && !neverScanned && group.hooks.length === 0;

  return (
    <section className="space-y-2">
      <div className="flex flex-wrap items-baseline justify-between gap-x-4 gap-y-1 px-0.5">
        <div className="flex min-w-0 flex-wrap items-center gap-2">
          <h3 className="truncate text-body font-semibold">{group.name}</h3>
          <Badge variant="outline" className="font-normal">
            {providerDisplayName(group.provider)}
          </Badge>
          <span
            className={cn(
              "text-caption",
              online ? "text-success" : "text-muted-foreground",
            )}
          >
            {online
              ? t(($) => $.hooks.runtime.online)
              : lastSeen
                ? t(($) => $.hooks.runtime.stale_since, { when: lastSeen })
                : t(($) => $.hooks.runtime.stale)}
          </span>
        </div>
        <span className="text-caption text-muted-foreground">
          {observedAt
            ? t(($) => $.hooks.runtime.observed_at, { when: observedAt })
            : t(($) => $.hooks.runtime.never_observed)}
        </span>
      </div>
      {group.host ? (
        <p className="px-0.5 text-caption text-muted-foreground">{group.host}</p>
      ) : null}
      <SettingsCard>
        {unsupported ? (
          <div className="flex items-start gap-2 px-4 py-3.5">
            <CircleSlash className="mt-0.5 size-4 shrink-0 text-muted-foreground" />
            <div className="min-w-0">
              <div className="text-body font-medium">
                {t(($) => $.hooks.runtime.unsupported)}
              </div>
              <p className="mt-0.5 text-caption leading-5 text-muted-foreground">
                {t(($) => $.hooks.runtime.unsupported_hint, {
                  provider: providerDisplayName(group.provider),
                })}
              </p>
            </div>
          </div>
        ) : neverScanned ? (
          <div className="px-4 py-3.5">
            <div className="text-body font-medium">
              {t(($) => $.hooks.runtime.never_scanned)}
            </div>
            <p className="mt-0.5 text-caption leading-5 text-muted-foreground">
              {t(($) => $.hooks.runtime.never_scanned_hint)}
            </p>
          </div>
        ) : noHooks ? (
          <div className="px-4 py-3.5">
            <div className="text-body font-medium">
              {t(($) => $.hooks.runtime.no_hooks)}
            </div>
            <p className="mt-0.5 text-caption leading-5 text-muted-foreground">
              {t(($) => $.hooks.runtime.no_hooks_hint)}
            </p>
          </div>
        ) : (
          group.hooks.map((hook) => <HookRow key={hook.id} hook={hook} />)
        )}
      </SettingsCard>
    </section>
  );
}

export function HooksTab() {
  const { t } = useT("settings");
  const wsId = useWorkspaceId();

  const { data, isPending } = useQuery(runtimeHookListOptions(wsId));
  const groups = useMemo(() => data?.runtimes ?? [], [data?.runtimes]);

  return (
    <SettingsTab
      title={t(($) => $.hooks.title)}
      description={t(($) => $.hooks.description)}
    >
      {isPending ? (
        <div className="flex items-center gap-2 px-0.5 text-body text-muted-foreground">
          <LoaderCircle className="size-4 animate-spin" />
          {t(($) => $.hooks.loading)}
        </div>
      ) : groups.length === 0 ? (
        <div className="flex flex-col items-center gap-2 rounded-lg border border-dashed border-surface-border px-6 py-10 text-center">
          <Webhook className="size-5 text-muted-foreground" />
          <div className="text-body font-medium">
            {t(($) => $.hooks.empty.title)}
          </div>
          <p className="max-w-md text-caption leading-5 text-muted-foreground">
            {t(($) => $.hooks.empty.description)}
          </p>
        </div>
      ) : (
        <div className="space-y-6">
          {groups.map((group) => (
            <RuntimeGroup key={group.runtime_id} group={group} />
          ))}
        </div>
      )}
    </SettingsTab>
  );
}
