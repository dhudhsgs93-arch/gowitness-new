import { Button } from "@/components/ui/button";
import { Card, CardContent, CardFooter } from "@/components/ui/card";
import { useEffect, useMemo, useState, useCallback, useRef } from "react";
import { Link, useSearchParams } from "react-router-dom";
import { WideSkeleton } from "@/components/loading";
import { Badge } from "@/components/ui/badge";
import {
  AlertOctagonIcon, BanIcon, CheckIcon, ClockIcon, ExternalLinkIcon,
  FilterIcon, GroupIcon, ShieldCheckIcon, XIcon, CheckCircle2Icon, AlertTriangleIcon, StarIcon, SkullIcon, Trash2Icon, MessageSquareIcon,
  LoaderIcon, CopyIcon, DownloadIcon, CheckSquareIcon, SquareIcon, ArrowDownUpIcon, EyeOffIcon, UndoIcon
} from "lucide-react";
import { Tooltip, TooltipContent, TooltipProvider, TooltipTrigger } from "@/components/ui/tooltip";
import { Popover, PopoverContent, PopoverTrigger } from "@/components/ui/popover";
import { Command, CommandEmpty, CommandGroup, CommandInput, CommandItem, CommandList } from "@/components/ui/command";
import { formatDistanceToNow, format } from 'date-fns';
import { cn } from "@/lib/utils";
import * as api from "@/lib/api/api";
import * as apitypes from "@/lib/api/types";
import { getWappalyzerData } from "./data";
import { getIconUrl, getStatusColor } from "@/lib/common";
import { Label } from "@/components/ui/label";
import { Switch } from "@/components/ui/switch";
import { Textarea } from "@/components/ui/textarea";
import { toast } from "@/hooks/use-toast";

const REVIEW_STATUSES = [
  { key: 'done', icon: CheckCircle2Icon, label: 'Done', color: 'text-green-500', bg: 'bg-green-500/10 border-green-500/30' },
  { key: 'attention', icon: AlertTriangleIcon, label: 'Attention', color: 'text-red-500', bg: 'bg-red-500/10 border-red-500/30' },
  { key: 'interesting', icon: StarIcon, label: 'Interesting', color: 'text-yellow-500', bg: 'bg-yellow-500/10 border-yellow-500/30' },
  { key: 'vuln', icon: SkullIcon, label: 'Vuln', color: 'text-purple-500', bg: 'bg-purple-500/10 border-purple-500/30' },
  { key: 'junk', icon: Trash2Icon, label: 'Junk', color: 'text-gray-500', bg: 'bg-gray-500/10 border-gray-500/30' },
] as const;

const BATCH_SIZE = 48;

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


const GalleryPage = () => {
  const [gallery, setGallery] = useState<apitypes.galleryResult[]>([]);
  const [wappalyzer, setWappalyzer] = useState<apitypes.wappalyzer>();
  const [technology, setTechnology] = useState<apitypes.technologylist>();
  const [totalCount, setTotalCount] = useState(0);
  const [loading, setLoading] = useState(true);
  const [loadingMore, setLoadingMore] = useState(false);
  const [reviewStats, setReviewStats] = useState<apitypes.reviewStats>();
  const [selected, setSelected] = useState<Set<number>>(new Set());
  const [selectMode, setSelectMode] = useState(false);
  const saveTimers = useRef<Record<number, ReturnType<typeof setTimeout>>>({});
  const [trashedHosts, setTrashedHosts] = useState<apitypes.trashedHost[]>([]);
  const [trashSuggestions, setTrashSuggestions] = useState<string[]>([]);
  const [trashInput, setTrashInput] = useState('');
  const trashDebounce = useRef<ReturnType<typeof setTimeout>>();
  const pageRef = useRef(1);
  const hasMoreRef = useRef(true);
  const sentinelRef = useRef<HTMLDivElement | null>(null);

  const [searchParams, setSearchParams] = useSearchParams();
  const technologyFilter = searchParams.get("technologies") || "";
  const statusFilter = searchParams.get("status") || "";
  const reviewFilter = searchParams.get("review") || "";
  const perceptionGroup = searchParams.get("perception") === "true";
  const showFailed = searchParams.get("failed") !== "false";
  const sortOrder = searchParams.get("sort") || "";

  const loadReviewStats = useCallback(async () => {
    try {
      const stats = await api.get('reviewStats');
      setReviewStats(stats);
    } catch { /* ignore */ }
  }, []);

  const loadTrashedHosts = useCallback(async () => {
    try {
      const hosts = await api.get('trashList');
      setTrashedHosts(hosts);
    } catch { /* ignore */ }
  }, []);

  const trashHost = async (host: string) => {
    if (!host.trim()) return;
    try {
      await api.post('trashAdd', { host: host.trim() });
      setTrashInput('');
      setTrashSuggestions([]);
      loadTrashedHosts();
      // Reload gallery to reflect hidden host
      pageRef.current = 1;
      hasMoreRef.current = true;
      loadBatch(1, true);
      loadReviewStats();
      toast({ title: `Hidden: ${host.trim()}`, duration: 2000 });
    } catch {
      toast({ title: "Error", description: "Failed to hide host", variant: "destructive" });
    }
  };

  const restoreHost = async (id: number) => {
    try {
      await api.post('trashRestore', { id });
      loadTrashedHosts();
      pageRef.current = 1;
      hasMoreRef.current = true;
      loadBatch(1, true);
      loadReviewStats();
      toast({ title: "Host restored", duration: 1500 });
    } catch {
      toast({ title: "Error", description: "Failed to restore host", variant: "destructive" });
    }
  };

  const searchTrashSuggestions = (q: string) => {
    setTrashInput(q);
    if (trashDebounce.current) clearTimeout(trashDebounce.current);
    trashDebounce.current = setTimeout(async () => {
      if (q.trim().length < 1) {
        setTrashSuggestions([]);
        return;
      }
      try {
        const suggestions = await api.get('trashSuggest', { q: q.trim() });
        setTrashSuggestions(suggestions);
      } catch {
        setTrashSuggestions([]);
      }
    }, 300);
  };

  // Load a batch of results
  const loadBatch = useCallback(async (page: number, reset: boolean) => {
    if (reset) {
      setLoading(true);
    } else {
      setLoadingMore(true);
    }
    try {
      const params: Record<string, string | number | boolean> = {
        page,
        limit: BATCH_SIZE,
        technologies: technologyFilter,
        status: statusFilter,
        perception: perceptionGroup ? 'true' : 'false',
        failed: showFailed ? 'true' : 'false',
      };
      if (reviewFilter) params.review = reviewFilter;
      if (sortOrder) params.sort = sortOrder;

      const s = await api.get('gallery', params);
      const newResults = s.results || [];
      setTotalCount(s.total_count);

      if (reset) {
        setGallery(newResults);
      } else {
        setGallery(prev => [...(prev || []), ...newResults]);
      }

      hasMoreRef.current = newResults.length === BATCH_SIZE && (page * BATCH_SIZE) < s.total_count;
    } catch (err) {
      toast({ title: "API Error", variant: "destructive", description: `Failed to get gallery: ${err}` });
    } finally {
      setLoading(false);
      setLoadingMore(false);
    }
  }, [technologyFilter, statusFilter, perceptionGroup, showFailed, reviewFilter, sortOrder]);

  // Initial load + reload on filter change
  useEffect(() => {
    pageRef.current = 1;
    hasMoreRef.current = true;
    setSelected(new Set());
    loadBatch(1, true);
    loadReviewStats();
  }, [technologyFilter, statusFilter, perceptionGroup, showFailed, reviewFilter, sortOrder]);

  useEffect(() => {
    getWappalyzerData(setWappalyzer, setTechnology);
    loadTrashedHosts();
  }, []);

  // Infinite scroll via IntersectionObserver
  useEffect(() => {
    const sentinel = sentinelRef.current;
    if (!sentinel) return;

    const observer = new IntersectionObserver(
      (entries) => {
        if (entries[0].isIntersecting && hasMoreRef.current && !loadingMore && !loading) {
          pageRef.current += 1;
          loadBatch(pageRef.current, false);
        }
      },
      { rootMargin: '600px' }
    );

    observer.observe(sentinel);
    return () => observer.disconnect();
  }, [loadBatch, loadingMore, loading]);

  const handleTechnologyChange = (tech: string) => {
    const field = "technologies";
    setSearchParams(prev => {
      const currentTechnology = prev.get(field)?.split(",").filter(Boolean) || [];
      if (currentTechnology.includes(tech)) {
        prev.set(field, currentTechnology.filter(s => s !== tech).join(","));
      } else {
        currentTechnology.push(tech);
        prev.set(field, currentTechnology.join(","));
      }
      return prev;
    });
  };

  const handleStatusFilter = (status: string) => {
    setSearchParams(prev => {
      const currentStatus = prev.get("status")?.split(",").filter(Boolean) || [];
      if (currentStatus.includes(status)) {
        prev.set("status", currentStatus.filter(s => s !== status).join(","));
      } else {
        currentStatus.push(status);
        prev.set("status", currentStatus.join(","));
      }
      return prev;
    });
  };

  const handleGroupBySimilar = () => {
    setSearchParams(prev => {
      prev.set("perception", (!perceptionGroup).toString());
      return prev;
    });
  };

  const handleToggleShowFailed = () => {
    setSearchParams(prev => {
      prev.set("failed", (!showFailed).toString());
      return prev;
    });
  };

  const handleSort = () => {
    setSearchParams(prev => {
      const current = prev.get("sort") || "";
      if (current === "") prev.set("sort", "newest");
      else if (current === "newest") prev.set("sort", "oldest");
      else prev.delete("sort");
      return prev;
    });
  };

  const handleReviewFilter = (filter: string) => {
    setSearchParams(prev => {
      if (prev.get("review") === filter) {
        prev.delete("review");
      } else {
        prev.set("review", filter);
      }
      return prev;
    });
  };

  const setReviewStatus = async (resultId: number, idx: number, newStatus: string) => {
    if (!gallery) return;
    const item = gallery[idx];
    const status = item.review_status === newStatus ? '' : newStatus;
    try {
      await api.post('reviewSet', { status, comment: item.review_comment || '' }, { id: resultId });
      setGallery(prev => prev?.map((g, i) => i === idx ? { ...g, review_status: status } : g));
      loadReviewStats();
    } catch {
      toast({ title: "Error", description: "Failed to save review", variant: "destructive" });
    }
  };

  const saveComment = async (resultId: number, idx: number, comment: string) => {
    if (!gallery) return;
    const item = gallery[idx];
    try {
      await api.post('reviewSet', { status: item.review_status || '', comment }, { id: resultId });
      setGallery(prev => prev?.map((g, i) => i === idx ? { ...g, review_comment: comment } : g));
      loadReviewStats();
    } catch {
      toast({ title: "Error", description: "Failed to save comment", variant: "destructive" });
    }
  };

  const handleCommentChange = (resultId: number, idx: number, value: string) => {
    setGallery(prev => prev?.map((g, i) => i === idx ? { ...g, review_comment: value } : g));
    if (saveTimers.current[resultId]) clearTimeout(saveTimers.current[resultId]);
    saveTimers.current[resultId] = setTimeout(() => saveComment(resultId, idx, value), 800);
  };

  // Bulk select
  const toggleSelect = (id: number) => {
    setSelected(prev => {
      const next = new Set(prev);
      if (next.has(id)) next.delete(id); else next.add(id);
      return next;
    });
  };

  const selectAll = () => {
    if (!gallery) return;
    setSelected(new Set(gallery.map(g => g.id)));
  };

  const selectNone = () => setSelected(new Set());

  const bulkTag = async (status: string) => {
    if (selected.size === 0) return;
    try {
      await api.post('reviewBulk', { ids: Array.from(selected), status });
      setGallery(prev => prev?.map(g => selected.has(g.id) ? { ...g, review_status: status } : g));
      setSelected(new Set());
      setSelectMode(false);
      loadReviewStats();
      toast({ title: `Tagged ${selected.size} items as ${status || 'cleared'}` });
    } catch {
      toast({ title: "Error", description: "Bulk tag failed", variant: "destructive" });
    }
  };

  // Copy URL
  const copyUrl = (url: string) => {
    navigator.clipboard.writeText(url);
    toast({ title: "Copied", description: url, duration: 1500 });
  };

  // Export URLs
  const exportUrls = () => {
    const params = new URLSearchParams();
    if (reviewFilter) params.set("review", reviewFilter);
    window.open(`/api/review/export-urls?${params.toString()}`, '_blank');
  };

  const sortedTechnologies = useMemo(() => {
    if (!technology) return [];
    const selectedTechnologies = technologyFilter.split(',').filter(Boolean);
    return [
      ...selectedTechnologies,
      ...technology.technologies.filter(tech => !selectedTechnologies.includes(tech))
    ];
  }, [technology, technologyFilter]);

  const renderGalleryCard = (screenshot: apitypes.galleryResult, idx: number) => {
    const probedDate = new Date(screenshot.probed_at);
    const timeAgo = formatDistanceToNow(probedDate, { addSuffix: true });
    const rawDate = format(probedDate, "PPpp");
    const isSelected = selected.has(screenshot.id);

    return (
      <div
        key={screenshot.id}
        className={cn("relative", selectMode && "cursor-pointer")}
        onClick={selectMode ? () => toggleSelect(screenshot.id) : undefined}
      >
        {selectMode && (
          <div
            className={cn(
              "absolute top-0 left-0 z-20 p-1.5 rounded-br-lg transition-colors pointer-events-none",
              isSelected ? "bg-blue-500 text-white" : "bg-black/60 text-gray-300"
            )}
          >
            {isSelected ? <CheckSquareIcon className="w-4 h-4" /> : <SquareIcon className="w-4 h-4" />}
          </div>
        )}
        <Card className={cn(
          "group overflow-hidden transition-all hover:shadow-lg flex flex-col h-full",
          getReviewBorderColor(screenshot.review_status),
          isSelected && selectMode && "ring-2 ring-blue-500"
        )}>
          {/* Review tag bar */}
          <div className="flex items-center gap-1 px-2 py-1 border-b" onClick={e => { if (selectMode) return; e.stopPropagation(); }}>
            {REVIEW_STATUSES.map(s => {
              const Icon = s.icon;
              const isActive = screenshot.review_status === s.key;
              return (
                <TooltipProvider key={s.key} delayDuration={0}>
                  <Tooltip>
                    <TooltipTrigger asChild>
                      <button
                        onClick={(e) => { if (selectMode) return; e.preventDefault(); setReviewStatus(screenshot.id, idx, s.key); }}
                        className={cn(
                          "p-1 rounded transition-all border",
                          isActive ? s.bg : "border-transparent hover:border-muted-foreground/30",
                          selectMode && "pointer-events-none"
                        )}
                      >
                        <Icon className={cn("w-3.5 h-3.5", isActive ? s.color : "text-muted-foreground")} />
                      </button>
                    </TooltipTrigger>
                    <TooltipContent side="bottom" className="text-xs">
                      <p>{s.label}</p>
                    </TooltipContent>
                  </Tooltip>
                </TooltipProvider>
              );
            })}
            {screenshot.review_comment && (
              <MessageSquareIcon className="w-3 h-3 text-blue-400 ml-auto" />
            )}
          </div>

          {selectMode ? (
            <CardContent className="p-0 relative flex-grow">
              {screenshot.failed ? (
                <div className="w-full h-48 bg-gray-800 flex items-center justify-center">
                  <XIcon className="text-gray-600 w-12 h-12" />
                </div>
              ) : (
                <img
                  src={screenshot.screenshot
                    ? `data:image/png;base64,${screenshot.screenshot}`
                    : api.endpoints.screenshot.path + "/" + screenshot.file_name}
                  alt={screenshot.url}
                  loading="lazy"
                  className="w-full h-48 object-cover"
                />
              )}
              <div className="absolute top-2 right-2">
                <Badge variant="default" className={`${getStatusColor(screenshot.response_code)}`}>
                  {screenshot.response_code}
                </Badge>
              </div>
            </CardContent>
          ) : (
            <Link to={`/screenshot/${screenshot.id}`}>
              <CardContent className="p-0 relative flex-grow">
                {screenshot.failed ? (
                  <div className="w-full h-48 bg-gray-800 flex items-center justify-center">
                    <XIcon className="text-gray-600 w-12 h-12" />
                  </div>
                ) : (
                  <img
                    src={screenshot.screenshot
                      ? `data:image/png;base64,${screenshot.screenshot}`
                      : api.endpoints.screenshot.path + "/" + screenshot.file_name}
                    alt={screenshot.url}
                    loading="lazy"
                    className="w-full h-48 object-cover transition-all duration-300 filter group-hover:scale-105"
                  />
                )}
                <div className="absolute top-2 right-2">
                  <Badge variant="default" className={`${getStatusColor(screenshot.response_code)}`}>
                    {screenshot.response_code}
                  </Badge>
                </div>
                <div className="absolute bottom-2 right-2 opacity-0 group-hover:opacity-100 transition-opacity">
                  <ExternalLinkIcon className="text-white drop-shadow-lg" />
                </div>
              </CardContent>
            </Link>
          )}

          <CardFooter className="p-2 flex flex-col items-start">
            <div className="w-full mb-1">
              <TooltipProvider>
                <Tooltip>
                  <TooltipTrigger asChild>
                    <div className="w-full truncate text-sm font-medium">
                      {screenshot.title || "Untitled"}
                    </div>
                  </TooltipTrigger>
                  <TooltipContent>
                    <p>{screenshot.title || "Untitled"}</p>
                  </TooltipContent>
                </Tooltip>
              </TooltipProvider>
              <div className="w-full flex items-center gap-1 mt-0.5">
                <div className="truncate text-xs text-muted-foreground flex-1">
                  {screenshot.url}
                </div>
                {!selectMode && (
                  <TooltipProvider delayDuration={0}>
                    <Tooltip>
                      <TooltipTrigger asChild>
                        <button
                          onClick={(e) => { e.stopPropagation(); e.preventDefault(); copyUrl(screenshot.url); }}
                          className="shrink-0 p-0.5 rounded hover:bg-muted transition-colors"
                        >
                          <CopyIcon className="w-3 h-3 text-muted-foreground hover:text-foreground" />
                        </button>
                      </TooltipTrigger>
                      <TooltipContent side="bottom" className="text-xs">
                        <p>Copy URL</p>
                      </TooltipContent>
                    </Tooltip>
                  </TooltipProvider>
                )}
              </div>
            </div>
            <div className="w-full flex items-center justify-between mt-1">
              <TooltipProvider delayDuration={0}>
                <Tooltip>
                  <TooltipTrigger asChild>
                    <div className="flex items-center space-x-1 text-xs text-muted-foreground">
                      <ClockIcon className="w-3 h-3" />
                      <span className="text-nowrap">{timeAgo}</span>
                    </div>
                  </TooltipTrigger>
                  <TooltipContent side="bottom" className="text-xs">
                    <p>{rawDate}</p>
                  </TooltipContent>
                </Tooltip>
              </TooltipProvider>
              <div className="flex flex-wrap justify-end gap-1">
                {screenshot.technologies?.map(tech => {
                  const iconUrl = getIconUrl(tech, wappalyzer);
                  return iconUrl ? (
                    <TooltipProvider key={tech} delayDuration={0}>
                      <Tooltip>
                        <TooltipTrigger asChild>
                          <div className="w-6 h-6 flex items-center justify-center">
                            <img
                              src={iconUrl}
                              alt={tech}
                              loading="lazy"
                              className="w-5 h-5 object-contain"
                            />
                          </div>
                        </TooltipTrigger>
                        <TooltipContent>
                          <p>{tech}</p>
                        </TooltipContent>
                      </Tooltip>
                    </TooltipProvider>
                  ) : null;
                })}
              </div>
            </div>
            {/* Comment area */}
            {!selectMode && (
              <div className="w-full mt-1" onClick={e => e.stopPropagation()}>
                <Textarea
                  placeholder="comment..."
                  value={screenshot.review_comment || ''}
                  onChange={(e) => handleCommentChange(screenshot.id, idx, e.target.value)}
                  className="text-xs min-h-[28px] max-h-[80px] resize-y font-mono"
                  rows={1}
                />
              </div>
            )}
          </CardFooter>
        </Card>
      </div>
    );
  };

  if (loading) return <WideSkeleton />;

  return (
    <div>
      <div className="sticky top-16 z-40 bg-background/95 backdrop-blur border-b pb-3 -mx-4 px-4 pt-3">
      {/* Bulk select bar */}
      {selectMode && (
        <div className="flex items-center gap-2 p-2 rounded-lg bg-muted border mb-2">
          <span className="text-sm font-medium">{selected.size} selected</span>
          <Button variant="outline" size="sm" onClick={selectAll} className="h-7 text-xs">All</Button>
          <Button variant="outline" size="sm" onClick={selectNone} className="h-7 text-xs">None</Button>
          <div className="border-l h-5 mx-1" />
          {REVIEW_STATUSES.map(s => {
            const Icon = s.icon;
            return (
              <TooltipProvider key={s.key} delayDuration={0}>
                <Tooltip>
                  <TooltipTrigger asChild>
                    <Button
                      variant="outline"
                      size="sm"
                      onClick={() => bulkTag(s.key)}
                      disabled={selected.size === 0}
                      className="h-7 text-xs"
                    >
                      <Icon className={cn("w-3 h-3 mr-1", s.color)} />
                      {s.label}
                    </Button>
                  </TooltipTrigger>
                  <TooltipContent className="text-xs"><p>Tag selected as {s.label}</p></TooltipContent>
                </Tooltip>
              </TooltipProvider>
            );
          })}
          <Button variant="outline" size="sm" onClick={() => bulkTag('')} disabled={selected.size === 0} className="h-7 text-xs">
            <XIcon className="w-3 h-3 mr-1" /> Clear tag
          </Button>
          <div className="flex-1" />
          <Button variant="ghost" size="sm" onClick={() => { setSelectMode(false); setSelected(new Set()); }} className="h-7 text-xs">
            Cancel
          </Button>
        </div>
      )}
      <div className="flex flex-wrap gap-4 items-center justify-between rounded-lg">
        <div className="flex flex-wrap gap-2">
          <Popover>
            <PopoverTrigger asChild>
              <Button variant="outline" className="w-[200px] justify-start">
                <FilterIcon className="mr-2 h-4 w-4" />
                {technologyFilter.split(',').filter(n => n).length > 0 ? (
                  <>
                    {technologyFilter.split(',').length} selected
                  </>
                ) : (
                  "Filter by Technology"
                )}
              </Button>
            </PopoverTrigger>
            <PopoverContent className="w-[200px] p-0">
              <Command>
                <CommandInput placeholder="Search technologies..." />
                <CommandList>
                  <CommandEmpty>No technology found.</CommandEmpty>
                  <CommandGroup>
                    {sortedTechnologies.map((tech) => (
                      <CommandItem
                        key={tech}
                        onSelect={() => handleTechnologyChange(tech)}
                      >
                        <CheckIcon
                          className={cn(
                            "mr-2 h-4 w-4",
                            technologyFilter.includes(tech) ? "opacity-100" : "opacity-0"
                          )}
                        />
                        {tech}
                      </CommandItem>
                    ))}
                  </CommandGroup>
                </CommandList>
              </Command>
            </PopoverContent>
          </Popover>
          <Button
            variant={statusFilter.includes("200") ? "secondary" : "outline"}
            onClick={() => handleStatusFilter("200")}
          >
            <ShieldCheckIcon className="mr-2 h-4 w-4" />
            200
          </Button>
          <Button
            variant={statusFilter.includes("403") ? "secondary" : "outline"}
            onClick={() => handleStatusFilter("403")}
          >
            <BanIcon className="mr-2 h-4 w-4" />
            403
          </Button>
          <Button
            variant={statusFilter.includes("500") ? "secondary" : "outline"}
            onClick={() => handleStatusFilter("500")}
          >
            <AlertOctagonIcon className="mr-2 h-4 w-4" />
            500
          </Button>
          <Button
            variant={perceptionGroup ? "secondary" : "outline"}
            onClick={handleGroupBySimilar}
          >
            <GroupIcon className="mr-2 h-4 w-4" />
            Group by Similar
          </Button>
          <div className="flex items-center space-x-2 p-2">
            <Switch
              id="show-failed"
              checked={showFailed}
              onCheckedChange={handleToggleShowFailed}
            />
            <Label htmlFor="show-failed" className="text-sm">
              Show Failed
            </Label>
          </div>
          {/* Review status filter pills */}
          <div className="flex items-center gap-1 border-l pl-2 ml-1">
            <Button
              variant={reviewFilter === '' ? "secondary" : "outline"}
              size="sm"
              onClick={() => handleReviewFilter('')}
              className="h-7 text-xs"
            >
              All
              {reviewStats && <span className="ml-1 opacity-60">{reviewStats.total}</span>}
            </Button>
            <Button
              variant={reviewFilter === 'unseen' ? "secondary" : "outline"}
              size="sm"
              onClick={() => handleReviewFilter('unseen')}
              className="h-7 text-xs"
            >
              Unseen
              {reviewStats && <span className="ml-1 opacity-60">{reviewStats.counts.unseen || 0}</span>}
            </Button>
            {REVIEW_STATUSES.map(s => {
              const Icon = s.icon;
              return (
                <Button
                  key={s.key}
                  variant={reviewFilter === s.key ? "secondary" : "outline"}
                  size="sm"
                  onClick={() => handleReviewFilter(s.key)}
                  className="h-7 text-xs"
                >
                  <Icon className={cn("w-3 h-3 mr-1", s.color)} />
                  {reviewStats?.counts[s.key] || 0}
                </Button>
              );
            })}
            <Button
              variant={reviewFilter === 'commented' ? "secondary" : "outline"}
              size="sm"
              onClick={() => handleReviewFilter('commented')}
              className="h-7 text-xs"
            >
              <MessageSquareIcon className="w-3 h-3 mr-1 text-blue-400" />
              {reviewStats?.commented || 0}
            </Button>
          </div>
        </div>
        <div className="flex items-center gap-2">
          <Button
            variant={sortOrder ? "secondary" : "outline"}
            size="sm"
            onClick={handleSort}
            className="h-7 text-xs"
          >
            <ArrowDownUpIcon className="w-3 h-3 mr-1" />
            {sortOrder === 'newest' ? 'Newest' : sortOrder === 'oldest' ? 'Oldest' : 'Sort'}
          </Button>
          <Button
            variant={selectMode ? "secondary" : "outline"}
            size="sm"
            onClick={() => { setSelectMode(!selectMode); if (selectMode) setSelected(new Set()); }}
            className="h-7 text-xs"
          >
            <CheckSquareIcon className="w-3 h-3 mr-1" />
            Select
          </Button>
          <Popover>
            <PopoverTrigger asChild>
              <Button variant="outline" size="sm" className="h-7 text-xs">
                <EyeOffIcon className="w-3 h-3 mr-1" />
                Hide host
                {trashedHosts.length > 0 && (
                  <Badge variant="secondary" className="ml-1 h-4 px-1 text-[10px]">{trashedHosts.length}</Badge>
                )}
              </Button>
            </PopoverTrigger>
            <PopoverContent className="w-[340px] p-0" align="end">
              <div className="p-3 border-b">
                <div className="text-sm font-medium mb-2">Hide host from gallery</div>
                <div className="relative">
                  <Command className="rounded-lg border" shouldFilter={false}>
                    <CommandInput
                      placeholder="Type hostname..."
                      value={trashInput}
                      onValueChange={searchTrashSuggestions}
                      onKeyDown={(e) => {
                        if (e.key === 'Enter' && trashInput.trim()) {
                          e.preventDefault();
                          trashHost(trashInput);
                        }
                      }}
                    />
                    {trashSuggestions.length > 0 && (
                      <CommandList>
                        <CommandGroup>
                          {trashSuggestions.map(host => (
                            <CommandItem key={host} onSelect={() => trashHost(host)} className="text-xs font-mono">
                              <EyeOffIcon className="w-3 h-3 mr-2 text-muted-foreground" />
                              {host}
                            </CommandItem>
                          ))}
                        </CommandGroup>
                      </CommandList>
                    )}
                  </Command>
                </div>
              </div>
              {trashedHosts.length > 0 && (
                <div className="max-h-[240px] overflow-y-auto">
                  <div className="px-3 py-2 text-xs text-muted-foreground font-medium">
                    Hidden hosts ({trashedHosts.length})
                  </div>
                  {trashedHosts.map(th => (
                    <div key={th.id} className="flex items-center justify-between px-3 py-1.5 hover:bg-muted text-xs group">
                      <div className="flex items-center gap-2 min-w-0 flex-1">
                        <EyeOffIcon className="w-3 h-3 text-muted-foreground shrink-0" />
                        <span className="font-mono truncate">{th.host}</span>
                        <span className="text-muted-foreground shrink-0">({th.count})</span>
                      </div>
                      <Button
                        variant="ghost"
                        size="sm"
                        onClick={() => restoreHost(th.id)}
                        className="h-5 px-1.5 text-xs opacity-0 group-hover:opacity-100"
                      >
                        <UndoIcon className="w-3 h-3 mr-1" />
                        Restore
                      </Button>
                    </div>
                  ))}
                </div>
              )}
            </PopoverContent>
          </Popover>
          <Button variant="outline" size="sm" onClick={exportUrls} className="h-7 text-xs">
            <DownloadIcon className="w-3 h-3 mr-1" />
            Export URLs
          </Button>
          <span className="text-sm text-muted-foreground">
            {gallery.length} / {totalCount}
          </span>
          {trashedHosts.length > 0 && (
            <span className="text-xs text-muted-foreground bg-muted px-2 py-0.5 rounded">
              <EyeOffIcon className="w-3 h-3 inline mr-1" />
              {trashedHosts.reduce((sum, h) => sum + h.count, 0)} hidden
            </span>
          )}
        </div>
      </div>
      </div>

      <div className="grid gap-6 md:grid-cols-2 lg:grid-cols-3 xl:grid-cols-4 pt-4">
        {gallery?.map((screenshot, idx) => renderGalleryCard(screenshot, idx))}
      </div>

      {/* Infinite scroll sentinel */}
      <div ref={sentinelRef} className="flex justify-center py-8">
        {loadingMore && (
          <div className="flex items-center gap-2 text-muted-foreground">
            <LoaderIcon className="w-5 h-5 animate-spin" />
            <span>Loading more...</span>
          </div>
        )}
        {!hasMoreRef.current && gallery.length > 0 && (
          <span className="text-muted-foreground text-sm">All {gallery.length} results loaded</span>
        )}
      </div>
    </div>
  );
};

export default GalleryPage;
