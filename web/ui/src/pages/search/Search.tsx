import { useEffect, useMemo, useRef, useState } from "react";
import { Link, useLoaderData, useNavigation } from "react-router-dom";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Textarea } from "@/components/ui/textarea";
import { Tooltip, TooltipContent, TooltipProvider, TooltipTrigger } from "@/components/ui/tooltip";
import {
  CheckCircle2Icon, AlertTriangleIcon, StarIcon, SkullIcon, Trash2Icon, MessageSquareIcon,
  CheckSquareIcon, SquareIcon, XIcon, EyeOffIcon,
} from "lucide-react";
import * as api from "@/lib/api/api";
import * as apitypes from "@/lib/api/types";
import { WideSkeleton } from "@/components/loading";
import { getStatusColor } from "@/lib/common";
import { cn } from "@/lib/utils";
import { toast } from "@/hooks/use-toast";

const REVIEW_STATUSES = [
  { key: 'done', icon: CheckCircle2Icon, label: 'Done', color: 'text-green-500', bg: 'bg-green-500/10 border-green-500/30' },
  { key: 'attention', icon: AlertTriangleIcon, label: 'Attention', color: 'text-red-500', bg: 'bg-red-500/10 border-red-500/30' },
  { key: 'interesting', icon: StarIcon, label: 'Interesting', color: 'text-yellow-500', bg: 'bg-yellow-500/10 border-yellow-500/30' },
  { key: 'vuln', icon: SkullIcon, label: 'Vuln', color: 'text-purple-500', bg: 'bg-purple-500/10 border-purple-500/30' },
  { key: 'junk', icon: Trash2Icon, label: 'Junk', color: 'text-gray-500', bg: 'bg-gray-500/10 border-gray-500/30' },
] as const;

const getReviewBorderColor = (status: string) => {
  switch (status) {
    case 'done': return 'border-l-4 border-l-green-500';
    case 'attention': return 'border-l-4 border-l-red-500';
    case 'interesting': return 'border-l-4 border-l-yellow-500';
    case 'vuln': return 'border-l-4 border-l-purple-500';
    case 'junk': return 'border-l-4 border-l-gray-500 opacity-50';
    default: return '';
  }
};

const hostOf = (u: string): string => {
  try { return new URL(u).hostname.toLowerCase(); } catch { return ""; }
};

export default function SearchResultsPage() {
  const data = useLoaderData() as apitypes.searchresult[] | { error: string } | undefined;
  const navigation = useNavigation();

  const [results, setResults] = useState<apitypes.searchresult[]>([]);
  const [reviewFilter, setReviewFilter] = useState<string>("");
  const [selectMode, setSelectMode] = useState(false);
  const [selected, setSelected] = useState<Set<number>>(new Set());
  const saveTimers = useRef<Record<number, ReturnType<typeof setTimeout>>>({});

  useEffect(() => {
    if (Array.isArray(data)) {
      setResults(data);
      setReviewFilter("");
    } else {
      setResults([]);
    }
    setSelected(new Set());
    setSelectMode(false);
  }, [data]);

  const counts = useMemo(() => {
    const c: Record<string, number> = { total: results.length, unseen: 0, commented: 0 };
    REVIEW_STATUSES.forEach(s => (c[s.key] = 0));
    for (const r of results) {
      if (r.review_status) c[r.review_status] = (c[r.review_status] || 0) + 1;
      else c.unseen += 1;
      if (r.review_comment) c.commented += 1;
    }
    return c;
  }, [results]);

  const filtered = useMemo(() => {
    if (!reviewFilter) return results;
    if (reviewFilter === 'unseen') return results.filter(r => !r.review_status);
    if (reviewFilter === 'commented') return results.filter(r => !!r.review_comment);
    return results.filter(r => r.review_status === reviewFilter);
  }, [results, reviewFilter]);

  const setReviewStatus = async (id: number, newStatus: string) => {
    const item = results.find(r => r.id === id);
    if (!item) return;
    const status = item.review_status === newStatus ? '' : newStatus;
    try {
      await api.post('reviewSet', { status, comment: item.review_comment || '' }, { id });
      setResults(prev => prev.map(r => r.id === id ? { ...r, review_status: status } : r));
    } catch {
      toast({ title: "Error", description: "Failed to save review", variant: "destructive" });
    }
  };

  const saveComment = async (id: number, comment: string) => {
    const item = results.find(r => r.id === id);
    if (!item) return;
    try {
      await api.post('reviewSet', { status: item.review_status || '', comment }, { id });
    } catch {
      toast({ title: "Error", description: "Failed to save comment", variant: "destructive" });
    }
  };

  const handleCommentChange = (id: number, value: string) => {
    setResults(prev => prev.map(r => r.id === id ? { ...r, review_comment: value } : r));
    if (saveTimers.current[id]) clearTimeout(saveTimers.current[id]);
    saveTimers.current[id] = setTimeout(() => saveComment(id, value), 800);
  };

  // ---- bulk select ----
  const toggleSelect = (id: number) => {
    setSelected(prev => {
      const next = new Set(prev);
      if (next.has(id)) next.delete(id); else next.add(id);
      return next;
    });
  };
  const selectAll = () => setSelected(new Set(filtered.map(r => r.id)));
  const selectNone = () => setSelected(new Set());

  const bulkTag = async (status: string) => {
    if (selected.size === 0) return;
    try {
      await api.post('reviewBulk', { ids: Array.from(selected), status });
      setResults(prev => prev.map(r => selected.has(r.id) ? { ...r, review_status: status } : r));
      setSelected(new Set());
      setSelectMode(false);
      toast({ title: `Tagged ${selected.size} as ${status || 'cleared'}` });
    } catch {
      toast({ title: "Error", description: "Bulk tag failed", variant: "destructive" });
    }
  };

  const bulkTrash = async () => {
    if (selected.size === 0) return;
    const n = selected.size;
    // hosts of the selected cards — used to drop them (and same-host siblings) from view
    const trashedHosts = new Set<string>();
    for (const r of results) if (selected.has(r.id)) { const h = hostOf(r.url); if (h) trashedHosts.add(h); }
    try {
      const res = await api.post('trashBulk', { ids: Array.from(selected) });
      setResults(prev => prev.filter(r => !trashedHosts.has(hostOf(r.url))));
      setSelected(new Set());
      setSelectMode(false);
      toast({ title: `Hid ${res?.hosts ?? n} host(s) from ${n} selected` });
    } catch {
      toast({ title: "Error", description: "Bulk trash failed", variant: "destructive" });
    }
  };

  if (navigation.state === 'loading') return <WideSkeleton />;

  if (!Array.isArray(data) || data.length === 0) {
    return <div className="text-center mt-8">No results found.</div>;
  }

  return (
    <div className="mx-auto p-4">
      <h1 className="text-2xl font-bold mb-3">
        Search Results <span className="text-muted-foreground">({filtered.length}{reviewFilter ? ` / ${results.length}` : ""})</span>
      </h1>

      <div className="sticky top-16 z-40 bg-background/95 backdrop-blur border-b mb-4 -mx-4 px-4 py-2">
        {/* Bulk select bar */}
        {selectMode && (
          <div className="flex flex-wrap items-center gap-2 p-2 rounded-lg bg-muted border mb-2">
            <span className="text-sm font-medium">{selected.size} selected</span>
            <Button variant="outline" size="sm" onClick={selectAll} className="h-7 text-xs">All</Button>
            <Button variant="outline" size="sm" onClick={selectNone} className="h-7 text-xs">None</Button>
            <div className="border-l h-5 mx-1" />
            {REVIEW_STATUSES.map(s => {
              const Icon = s.icon;
              return (
                <Button key={s.key} variant="outline" size="sm" onClick={() => bulkTag(s.key)} disabled={selected.size === 0} className="h-7 text-xs">
                  <Icon className={cn("w-3 h-3 mr-1", s.color)} />{s.label}
                </Button>
              );
            })}
            <Button variant="outline" size="sm" onClick={() => bulkTag('')} disabled={selected.size === 0} className="h-7 text-xs">
              <XIcon className="w-3 h-3 mr-1" /> Clear tag
            </Button>
            <div className="border-l h-5 mx-1" />
            <Button variant="destructive" size="sm" onClick={bulkTrash} disabled={selected.size === 0} className="h-7 text-xs">
              <EyeOffIcon className="w-3 h-3 mr-1" /> Trash hosts
            </Button>
            <div className="flex-1" />
            <Button variant="ghost" size="sm" onClick={() => { setSelectMode(false); setSelected(new Set()); }} className="h-7 text-xs">Cancel</Button>
          </div>
        )}

        {/* Review status filter pills + Select toggle */}
        <div className="flex flex-wrap items-center gap-1">
          <Button variant={reviewFilter === '' ? "secondary" : "outline"} size="sm" onClick={() => setReviewFilter('')} className="h-7 text-xs">
            All <span className="ml-1 opacity-60">{counts.total}</span>
          </Button>
          <Button variant={reviewFilter === 'unseen' ? "secondary" : "outline"} size="sm" onClick={() => setReviewFilter('unseen')} className="h-7 text-xs">
            Unseen <span className="ml-1 opacity-60">{counts.unseen}</span>
          </Button>
          {REVIEW_STATUSES.map(s => {
            const Icon = s.icon;
            return (
              <Button key={s.key} variant={reviewFilter === s.key ? "secondary" : "outline"} size="sm" onClick={() => setReviewFilter(s.key)} className="h-7 text-xs">
                <Icon className={cn("w-3 h-3 mr-1", s.color)} />{counts[s.key] || 0}
              </Button>
            );
          })}
          <Button variant={reviewFilter === 'commented' ? "secondary" : "outline"} size="sm" onClick={() => setReviewFilter('commented')} className="h-7 text-xs">
            <MessageSquareIcon className="w-3 h-3 mr-1 text-blue-400" />{counts.commented}
          </Button>
          <div className="flex-1" />
          <Button variant={selectMode ? "secondary" : "outline"} size="sm" onClick={() => { setSelectMode(!selectMode); if (selectMode) setSelected(new Set()); }} className="h-7 text-xs">
            <CheckSquareIcon className="w-3 h-3 mr-1" /> Select
          </Button>
        </div>
      </div>

      <div className="grid gap-4 md:grid-cols-2 lg:grid-cols-4">
        {filtered.map((result) => {
          const isSelected = selected.has(result.id);
          return (
            <Card
              key={result.id}
              className={cn(
                "flex flex-col h-full transition-shadow hover:shadow-lg group",
                getReviewBorderColor(result.review_status),
                selectMode && "cursor-pointer",
                isSelected && selectMode && "ring-2 ring-blue-500"
              )}
              onClick={selectMode ? () => toggleSelect(result.id) : undefined}
            >
              {/* Review tag bar */}
              <div className="flex items-center gap-1 px-2 py-1 border-b" onClick={e => { if (selectMode) return; e.stopPropagation(); }}>
                {REVIEW_STATUSES.map(s => {
                  const Icon = s.icon;
                  const isActive = result.review_status === s.key;
                  return (
                    <TooltipProvider key={s.key} delayDuration={0}>
                      <Tooltip>
                        <TooltipTrigger asChild>
                          <button
                            onClick={(e) => { if (selectMode) return; e.preventDefault(); setReviewStatus(result.id, s.key); }}
                            className={cn(
                              "p-1 rounded transition-all border",
                              isActive ? s.bg : "border-transparent hover:border-muted-foreground/30",
                              selectMode && "pointer-events-none"
                            )}
                          >
                            <Icon className={cn("w-3.5 h-3.5", isActive ? s.color : "text-muted-foreground")} />
                          </button>
                        </TooltipTrigger>
                        <TooltipContent side="bottom" className="text-xs"><p>{s.label}</p></TooltipContent>
                      </Tooltip>
                    </TooltipProvider>
                  );
                })}
                {selectMode && (
                  <span className="ml-auto">
                    {isSelected ? <CheckSquareIcon className="w-4 h-4 text-blue-500" /> : <SquareIcon className="w-4 h-4 text-muted-foreground" />}
                  </span>
                )}
                {!selectMode && result.review_comment && <MessageSquareIcon className="w-3 h-3 text-blue-400 ml-auto" />}
              </div>

              {selectMode ? (
                <CardHeader className="relative p-0">
                  <img
                    src={result.screenshot ? `data:image/png;base64,${result.screenshot}` : api.endpoints.screenshot.path + "/" + result.file_name}
                    alt={result.url} loading="lazy" className="w-full h-48 object-cover"
                  />
                  <Badge className={`absolute top-2 right-2 ${getStatusColor(result.response_code)} text-white`}>{result.response_code}</Badge>
                </CardHeader>
              ) : (
                <Link to={`/screenshot/${result.id}`}>
                  <CardHeader className="relative p-0">
                    <img
                      src={result.screenshot ? `data:image/png;base64,${result.screenshot}` : api.endpoints.screenshot.path + "/" + result.file_name}
                      alt={result.url} loading="lazy" className="w-full h-48 object-cover transition-all duration-300 filter group-hover:scale-105"
                    />
                    <Badge className={`absolute top-2 right-2 ${getStatusColor(result.response_code)} text-white`}>{result.response_code}</Badge>
                  </CardHeader>
                </Link>
              )}

              <CardContent className="flex-grow p-4">
                <CardTitle className="text-lg mb-2 line-clamp-2">{result.title}</CardTitle>
                <p className="text-sm text-muted-foreground mb-2 line-clamp-1">{result.final_url}</p>
                <div className="mb-2">
                  <div className="flex flex-wrap gap-2">
                    <p className="text-sm font-semibold mb-1">Matched Fields:</p>
                    {result.matched_fields.map((field) => (
                      <Badge key={field} variant="outline">{field}</Badge>
                    ))}
                  </div>
                </div>
                {!selectMode && (
                  <div onClick={e => e.stopPropagation()}>
                    <Textarea
                      placeholder="comment..."
                      value={result.review_comment || ''}
                      onChange={(e) => handleCommentChange(result.id, e.target.value)}
                      className="text-xs min-h-[28px] max-h-[80px] resize-y font-mono mt-1"
                      rows={1}
                    />
                  </div>
                )}
              </CardContent>
            </Card>
          );
        })}
      </div>
    </div>
  );
}
