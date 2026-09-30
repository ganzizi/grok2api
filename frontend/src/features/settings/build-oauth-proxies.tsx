import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { Activity, Eye, EyeOff, MoreHorizontal, Pencil, Plus, Trash2 } from "lucide-react";
import { useState } from "react";
import { useTranslation } from "react-i18next";
import { toast } from "sonner";

import { AlertDialog, AlertDialogAction, AlertDialogCancel, AlertDialogContent, AlertDialogDescription, AlertDialogFooter, AlertDialogHeader, AlertDialogTitle } from "@/components/ui/alert-dialog";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle } from "@/components/ui/dialog";
import { DropdownMenu, DropdownMenuContent, DropdownMenuItem, DropdownMenuSeparator, DropdownMenuTrigger } from "@/components/ui/dropdown-menu";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Spinner } from "@/components/ui/spinner";
import { Switch } from "@/components/ui/switch";
import { Table, TableActionCell, TableActionHead, TableBody, TableCell, TableHead, TableHeader, TableRow } from "@/components/ui/table";
import { Tooltip, TooltipContent, TooltipTrigger } from "@/components/ui/tooltip";
import { createBuildOAuthProxy, deleteBuildOAuthProxy, getBuildOAuthProxyURL, listBuildOAuthProxies, testBuildOAuthProxy, testBuildOAuthProxyURL, updateBuildOAuthProxy, type BuildOAuthProxyDTO, type BuildOAuthProbeResultDTO } from "@/features/settings/settings-api";
import { ErrorState, TableLoadingRow } from "@/shared/components/data-state";
import { formatCompactDateTime } from "@/shared/lib/format";
import { cn } from "@/shared/lib/cn";

type ProxyForm = { name: string; proxyURL: string; enabled: boolean };
const emptyForm: ProxyForm = { name: "", proxyURL: "", enabled: true };

export function BuildOAuthProxies() {
  const { t, i18n } = useTranslation();
  const queryClient = useQueryClient();
  const [editing, setEditing] = useState<BuildOAuthProxyDTO | null | undefined>(undefined);
  const [deleting, setDeleting] = useState<BuildOAuthProxyDTO | undefined>();
  const [form, setForm] = useState<ProxyForm>(emptyForm);
  const [proxyVisible, setProxyVisible] = useState(false);
  const [revealedProxyURL, setRevealedProxyURL] = useState("");
  const [draftProbe, setDraftProbe] = useState<BuildOAuthProbeResultDTO | undefined>();
  const query = useQuery({
    queryKey: ["build-oauth-proxies"],
    queryFn: listBuildOAuthProxies,
  });
  const items = query.data?.items ?? [];
  const enabledCount = items.filter((item) => item.enabled).length;

  const save = useMutation({
    mutationFn: () => {
      const proxyURL = form.proxyURL.trim();
      const input = { name: form.name.trim(), enabled: form.enabled, proxyURL: proxyURL && proxyURL !== revealedProxyURL ? proxyURL : undefined };
      return editing ? updateBuildOAuthProxy(editing.id, input) : createBuildOAuthProxy({ ...input, proxyURL });
    },
    onSuccess: () => {
      void queryClient.invalidateQueries({ queryKey: ["build-oauth-proxies"] });
      setEditing(undefined);
      toast.success(t("buildOAuthProxies.saved"));
    },
    onError: (error) => toast.error(error instanceof Error ? error.message : t("settings.egress.operationFailed")),
  });
  const reveal = useMutation({
    mutationFn: () => {
      if (!editing) throw new Error(t("buildOAuthProxies.revealUnavailable"));
      return getBuildOAuthProxyURL(editing.id);
    },
    onSuccess: ({ proxyURL }) => {
      setRevealedProxyURL(proxyURL);
      setProxyVisible(true);
      setForm((current) => ({ ...current, proxyURL }));
    },
    onError: (error) => toast.error(error instanceof Error ? error.message : t("settings.egress.operationFailed")),
  });
  const toggle = useMutation({
    mutationFn: (value: BuildOAuthProxyDTO) => updateBuildOAuthProxy(value.id, { name: value.name, enabled: !value.enabled }),
    onSuccess: () => {
      void queryClient.invalidateQueries({ queryKey: ["build-oauth-proxies"] });
    },
    onError: (error) => toast.error(error instanceof Error ? error.message : t("settings.egress.operationFailed")),
  });
  const remove = useMutation({
    mutationFn: (id: string) => deleteBuildOAuthProxy(id),
    onSuccess: () => {
      void queryClient.invalidateQueries({ queryKey: ["build-oauth-proxies"] });
      setDeleting(undefined);
      toast.success(t("buildOAuthProxies.deleted"));
    },
    onError: (error) => toast.error(error instanceof Error ? error.message : t("settings.egress.operationFailed")),
  });
  const testSaved = useMutation({
    mutationFn: testBuildOAuthProxy,
    onSuccess: (result) => {
      void queryClient.invalidateQueries({ queryKey: ["build-oauth-proxies"] });
      announceProbe(result);
    },
    onError: (error) => toast.error(error instanceof Error ? error.message : t("settings.egress.operationFailed")),
  });
  const testDraft = useMutation({
    mutationFn: async () => {
      const proxyURL = form.proxyURL.trim();
      if (proxyURL && proxyURL !== revealedProxyURL) {
        return testBuildOAuthProxyURL(proxyURL);
      }
      if (editing) return testBuildOAuthProxy(editing.id);
      throw new Error(t("buildOAuthProxies.testNeedURL"));
    },
    onSuccess: (result) => {
      setDraftProbe(result);
      void queryClient.invalidateQueries({ queryKey: ["build-oauth-proxies"] });
      announceProbe(result);
    },
    onError: (error) => toast.error(error instanceof Error ? error.message : t("settings.egress.operationFailed")),
  });
  const testEnabled = useMutation({
    mutationFn: async () => {
      const targets = items.filter((item) => item.enabled);
      let healthy = 0;
      let unhealthy = 0;
      for (const item of targets) {
        const result = await testBuildOAuthProxy(item.id);
        if (result.status === "healthy") healthy += 1;
        else unhealthy += 1;
      }
      return { healthy, unhealthy, total: targets.length };
    },
    onSuccess: (value) => {
      void queryClient.invalidateQueries({ queryKey: ["build-oauth-proxies"] });
      toast.success(t("buildOAuthProxies.testAllDone", value));
    },
    onError: (error) => toast.error(error instanceof Error ? error.message : t("settings.egress.operationFailed")),
  });

  function announceProbe(result: BuildOAuthProbeResultDTO) {
    if (result.status === "healthy") {
      toast.success(t("buildOAuthProxies.testOK", { ip: result.exitIp || t("buildOAuthProxies.healthy"), latency: result.latencyMs }));
      return;
    }
    toast.error(t("buildOAuthProxies.testFail", { error: result.error || t("settings.egress.operationFailed") }));
  }

  function openCreate() {
    setForm(emptyForm);
    setProxyVisible(false);
    setRevealedProxyURL("");
    setDraftProbe(undefined);
    setEditing(null);
  }

  function openEdit(value: BuildOAuthProxyDTO) {
    setForm({ name: value.name, proxyURL: "", enabled: value.enabled });
    setProxyVisible(false);
    setRevealedProxyURL("");
    setDraftProbe(undefined);
    setEditing(value);
  }

  const testingId = testSaved.isPending ? testSaved.variables : undefined;
  const busy = save.isPending || testSaved.isPending || testEnabled.isPending || testDraft.isPending;

  return (
    <section className="space-y-3">
      <div className="flex min-h-8 items-start justify-between gap-3 px-1">
        <div className="min-w-0">
          <div className="flex flex-wrap items-center gap-2">
            <h2 className="text-sm font-medium tracking-tight">{t("buildOAuthProxies.title")}</h2>
            <Badge variant={enabledCount > 0 ? "secondary" : "outline"} className="text-[10px]">
              {enabledCount > 0 ? t("buildOAuthProxies.enabledCount", { count: enabledCount }) : t("buildOAuthProxies.usingBuild")}
            </Badge>
          </div>
          <p className="mt-1 max-w-2xl text-xs leading-5 text-muted-foreground">{t("buildOAuthProxies.description")}</p>
        </div>
        <div className="flex shrink-0 flex-wrap items-center justify-end gap-2">
          {enabledCount > 0 ? (
            <Button type="button" size="sm" variant="ghost" disabled={busy} onClick={() => testEnabled.mutate()}>
              {testEnabled.isPending ? <Spinner /> : <Activity />}
              {t("buildOAuthProxies.testAll")}
            </Button>
          ) : null}
          <Button type="button" size="sm" variant="secondary" onClick={openCreate}><Plus />{t("buildOAuthProxies.add")}</Button>
        </div>
      </div>

      <div className="overflow-hidden rounded-lg border bg-card/40">
        {query.isError ? <ErrorState message={query.error.message} onRetry={() => void query.refetch()} /> : null}
        {!query.isError ? (
          <Table className="table-fixed">
            <TableHeader>
              <TableRow className="hover:bg-transparent">
                <TableHead className="w-[22%]">{t("buildOAuthProxies.name")}</TableHead>
                <TableHead>{t("buildOAuthProxies.endpoint")}</TableHead>
                <TableHead className="w-40">{t("buildOAuthProxies.status")}</TableHead>
                <TableHead className="w-20">{t("buildOAuthProxies.enabled")}</TableHead>
                <TableActionHead />
              </TableRow>
            </TableHeader>
            {query.isPending ? <TableBody><TableLoadingRow colSpan={5} /></TableBody> : null}
            {!query.isPending && items.length === 0 ? (
              <TableBody>
                <TableRow>
                  <TableCell colSpan={5} className="h-32">
                    <div className="flex flex-col items-center justify-center gap-2 py-4 text-center">
                      <p className="text-sm font-medium">{t("buildOAuthProxies.emptyTitle")}</p>
                      <p className="max-w-md text-xs leading-5 text-muted-foreground">{t("buildOAuthProxies.empty")}</p>
                      <Button type="button" size="sm" variant="secondary" className="mt-1" onClick={openCreate}><Plus />{t("buildOAuthProxies.add")}</Button>
                    </div>
                  </TableCell>
                </TableRow>
              </TableBody>
            ) : null}
            {!query.isPending && items.length > 0 ? (
              <TableBody>
                {items.map((value) => (
                  <TableRow key={value.id} className="h-14">
                    <TableCell>
                      <div className="flex min-w-0 items-center gap-2">
                        <span className={cn("size-1.5 shrink-0 rounded-full", value.enabled ? "bg-emerald-500" : "bg-muted-foreground/35")} />
                        <div className="min-w-0">
                          <span className={cn("block truncate text-xs font-medium", !value.enabled && "text-muted-foreground")} title={value.name}>{value.name}</span>
                          {value.accountBoundProxy ? <p className="text-[10px] text-muted-foreground">{t("buildOAuthProxies.accountBound")}</p> : null}
                        </div>
                      </div>
                    </TableCell>
                    <TableCell>
                      <div className="min-w-0" title={`${value.proxyDisplay || ""} · ${value.proxyFingerprint || ""}`}>
                        <p className="truncate font-mono text-xs">{value.proxyDisplay}</p>
                        {value.proxyFingerprint ? <p className="font-mono text-[10px] text-muted-foreground">#{value.proxyFingerprint}</p> : null}
                      </div>
                    </TableCell>
                    <TableCell>
                      <ProbeCell value={value} locale={i18n.language} testing={testingId === value.id} />
                    </TableCell>
                    <TableCell>
                      <Switch checked={value.enabled} disabled={toggle.isPending} onCheckedChange={() => toggle.mutate(value)} aria-label={t("buildOAuthProxies.enabled")} />
                    </TableCell>
                    <TableActionCell>
                      <div className="flex items-center justify-end gap-1">
                        <Tooltip>
                          <TooltipTrigger asChild>
                            <Button type="button" variant="ghost" size="sm" className="h-8 px-2 text-xs" disabled={busy} onClick={() => testSaved.mutate(value.id)}>
                              {testingId === value.id ? <Spinner /> : <Activity />}
                              {t("buildOAuthProxies.test")}
                            </Button>
                          </TooltipTrigger>
                          <TooltipContent>{t("buildOAuthProxies.testHint")}</TooltipContent>
                        </Tooltip>
                        <DropdownMenu>
                          <DropdownMenuTrigger asChild>
                            <Button type="button" variant="ghost" size="icon" className="size-8" aria-label={t("common.actions")}><MoreHorizontal /></Button>
                          </DropdownMenuTrigger>
                          <DropdownMenuContent align="end">
                            <DropdownMenuItem onClick={() => openEdit(value)}><Pencil />{t("common.edit")}</DropdownMenuItem>
                            <DropdownMenuSeparator />
                            <DropdownMenuItem className="text-destructive focus:text-destructive" onClick={() => setDeleting(value)}><Trash2 />{t("common.delete")}</DropdownMenuItem>
                          </DropdownMenuContent>
                        </DropdownMenu>
                      </div>
                    </TableActionCell>
                  </TableRow>
                ))}
              </TableBody>
            ) : null}
          </Table>
        ) : null}
      </div>

      <Dialog open={editing !== undefined} onOpenChange={(open) => { if (!open) setEditing(undefined); }}>
        <DialogContent className="overflow-y-auto sm:max-w-[540px]">
          <DialogHeader>
            <DialogTitle>{editing ? t("buildOAuthProxies.editTitle") : t("buildOAuthProxies.addTitle")}</DialogTitle>
            <DialogDescription>{t("buildOAuthProxies.dialogDescription")}</DialogDescription>
          </DialogHeader>
          <form className="space-y-3.5" onSubmit={(event) => { event.preventDefault(); event.stopPropagation(); save.mutate(); }}>
            <div className="space-y-2">
              <Label htmlFor="build-oauth-proxy-name">{t("buildOAuthProxies.name")}</Label>
              <Input id="build-oauth-proxy-name" autoComplete="off" data-1p-ignore data-lpignore="true" data-form-type="other" value={form.name} onChange={(event) => setForm({ ...form, name: event.target.value })} />
            </div>
            <div className="space-y-2">
              <Label htmlFor="build-oauth-proxy-url">{t("settings.egress.proxyURL")}</Label>
              <div className="flex gap-2">
                <Input id="build-oauth-proxy-url" type={proxyVisible ? "text" : "password"} autoComplete="off" data-1p-ignore data-lpignore="true" data-form-type="other" placeholder={editing ? t("settings.egress.keepConfigured") : t("buildOAuthProxies.example")} value={form.proxyURL} onChange={(event) => { setForm({ ...form, proxyURL: event.target.value }); setDraftProbe(undefined); }} />
                {editing ? (
                  <Button type="button" variant="outline" size="icon" className="shrink-0" disabled={reveal.isPending} aria-label={t(proxyVisible ? "buildOAuthProxies.hide" : "buildOAuthProxies.reveal")} onClick={() => { if (revealedProxyURL) setProxyVisible((visible) => !visible); else reveal.mutate(); }}>
                    {reveal.isPending ? <Spinner /> : proxyVisible ? <EyeOff /> : <Eye />}
                  </Button>
                ) : null}
              </div>
              <p className="text-xs leading-5 text-muted-foreground">{t("buildOAuthProxies.proxyHelp")}</p>
            </div>
            <div className="flex min-h-10 items-center justify-between gap-4 rounded-md bg-muted/45 px-3">
              <div className="min-w-0">
                <Label htmlFor="build-oauth-proxy-enabled" className="text-xs font-medium">{t("buildOAuthProxies.enabled")}</Label>
                <p className="text-[11px] text-muted-foreground">{t("buildOAuthProxies.enabledHelp")}</p>
              </div>
              <Switch id="build-oauth-proxy-enabled" checked={form.enabled} onCheckedChange={(enabled) => setForm({ ...form, enabled })} />
            </div>
            {draftProbe ? <DraftProbe result={draftProbe} /> : null}
            <DialogFooter className="gap-2 sm:justify-between">
              <Button type="button" variant="outline" size="sm" disabled={testDraft.isPending || (!form.proxyURL.trim() && !editing)} onClick={() => testDraft.mutate()}>
                {testDraft.isPending ? <Spinner /> : <Activity />}
                {t("buildOAuthProxies.test")}
              </Button>
              <div className="flex gap-2">
                <Button type="button" variant="secondary" size="sm" onClick={() => setEditing(undefined)}>{t("common.cancel")}</Button>
                <Button type="submit" size="sm" disabled={save.isPending || !form.name.trim() || (!editing && !form.proxyURL.trim())}>{save.isPending ? <Spinner /> : null}{t("common.save")}</Button>
              </div>
            </DialogFooter>
          </form>
        </DialogContent>
      </Dialog>

      <AlertDialog open={Boolean(deleting)} onOpenChange={(next) => { if (!next) setDeleting(undefined); }}>
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>{t("buildOAuthProxies.deleteTitle")}</AlertDialogTitle>
            <AlertDialogDescription>{t("buildOAuthProxies.deleteDescription", { name: deleting?.name })}</AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel disabled={remove.isPending}>{t("common.cancel")}</AlertDialogCancel>
            <AlertDialogAction className="bg-destructive text-white hover:bg-destructive/90" disabled={remove.isPending} onClick={(event) => { event.preventDefault(); if (deleting) remove.mutate(deleting.id); }}>
              {remove.isPending ? <Spinner /> : null}{t("common.delete")}
            </AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
    </section>
  );
}

function ProbeCell({ value, locale, testing }: { value: BuildOAuthProxyDTO; locale: string; testing: boolean }) {
  const { t } = useTranslation();
  if (testing) {
    return <span className="inline-flex items-center gap-1.5 text-[11px] text-muted-foreground"><Spinner />{t("buildOAuthProxies.testing")}</span>;
  }
  const healthy = value.probeStatus === "healthy";
  const unhealthy = value.probeStatus === "unhealthy";
  let label = t("buildOAuthProxies.neverTested");
  let detail = "";
  let dotClass = "bg-muted-foreground/35";
  let textClass = "text-muted-foreground";
  if (healthy) {
    label = value.exitIp || t("buildOAuthProxies.healthy");
    detail = value.exitIp ? `${value.exitIp} · ${value.probeLatencyMs}ms` : `${value.probeLatencyMs}ms`;
    dotClass = "bg-emerald-500";
    textClass = "text-foreground";
  } else if (unhealthy) {
    label = t("buildOAuthProxies.unhealthy");
    detail = value.probeError || t("buildOAuthProxies.unhealthy");
    dotClass = "bg-destructive";
    textClass = "text-destructive";
  }
  return (
    <Tooltip>
      <TooltipTrigger asChild>
        <span className="flex min-w-0 cursor-help items-center gap-1.5">
          <span className={cn("size-1.5 shrink-0 rounded-full", dotClass)} />
          <span className="min-w-0">
            <span className={cn("block text-xs", textClass)}>{label}</span>
            {value.lastProbedAt ? <span className="block text-[10px] text-muted-foreground">{formatCompactDateTime(value.lastProbedAt, locale)}</span> : null}
          </span>
        </span>
      </TooltipTrigger>
      <TooltipContent className="max-w-80">{detail || t("buildOAuthProxies.testHint")}</TooltipContent>
    </Tooltip>
  );
}

function DraftProbe({ result }: { result: BuildOAuthProbeResultDTO }) {
  const { t } = useTranslation();
  const ok = result.status === "healthy";
  return (
    <div className={cn("rounded-md px-3 py-2 text-xs leading-5", ok ? "bg-emerald-500/10 text-emerald-700 dark:text-emerald-300" : "bg-destructive/10 text-destructive")}>
      {ok
        ? t("buildOAuthProxies.testOK", { ip: result.exitIp || t("buildOAuthProxies.healthy"), latency: result.latencyMs })
        : t("buildOAuthProxies.testFail", { error: result.error || t("settings.egress.operationFailed") })}
    </div>
  );
}
