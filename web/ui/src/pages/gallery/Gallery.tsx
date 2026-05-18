import { Button } from "@/components/ui/button";
import { Card, CardContent, CardFooter } from "@/components/ui/card";
import { useEffect, useMemo, useState, useCallback, useRef } from "react";
import { Link, useSearchParams } from "react-router-dom";
import { WideSkeleton } from "@/components/loading";
import { Badge } from "@/components/ui/badge";
import {
  AlertOctagonIcon, BanIcon, CheckIcon, ClockIcon, ExternalLinkIcon,
  FilterIcon, GroupIcon, ShieldCheckIcon, XIcon, CheckCircle2Icon, AlertTriangleIcon, StarIcon, SkullIcon, Trash2Icon, MessageSquareIcon,
  LoaderIcon
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
  { key: 'done', icon: CheckCircle2Icon, label: 'Done', color: 'text-green-500', bg: 'bg-green-500/10 border-green-500/30', hotkey: '1' },
  { key: 'attention', icon: AlertTriangleIcon, label: 'Attention', color: 'text-red-500', bg: 'bg-red-500/10 border-red-500/30', hotkey: '2' },
  { key: 'interesting', icon: StarIcon, label: 'Interesting', color: 'text-yellow-500', bg: 'bg-yellow-500/10 border-yellow-500/30', hotkey: '3' },
  { key: 'vuln', icon: SkullIcon, label: 'Vuln', color: 'text-purple-500', bg: 'bg-purple-500/10 border-purple-500/30', hotkey: '4' },
  { key: 'junk', icon: Trash2Icon, label: 'Junk', color: 'text-gray-500', bg: 'bg-gray-500/10 border-gray-500/30', hotkey: '5' },
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
  const [focusedIdx, setFocusedIdx] = useState(-1);
  const cardRefs = useRef<(HTMLDivElement | null)[]>([]);
  const saveTimers = useRef<Record<number, ReturnType<typeof setTimeout>>>({});
  const pageRef = useRef(1);
  const hasMoreRef = useRef(true);
  const sentinelRef = useRef<HTMLDivElement | null>(null);

  const [searchParams, setSearchParams] = useSearchParams();
  const technologyFilter = searchParams.get("technologies") || "";
  const statusFilter = searchParams.get("status") || "";
  const reviewFilter = searchParams.get("review") || "";
  const perceptionGroup = searchParams.get("perception") === "true";
  const showFailed = searchParams.get("failed") !== "false";

  const loadReviewStats = useCallback(async () => {
    try {
      const stats = await api.get('reviewStats');
      setReviewStats(stats);
    } catch { /* ignore */ }
  }, []);

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
  }, [technologyFilter, statusFilter, perceptionGroup, showFailed, reviewFilter]);

  // Initial load + reload on filter change
  useEffect(() => {
    pageRef.current = 1;
    hasMoreRef.current = true;
    loadBatch(1, true);
    loadReviewStats();
  }, [technologyFilter, statusFilter, perceptionGroup, showFailed, reviewFilter]);

  useEffect(() => {
    getWappalyzerData(setWappalyzer, setTechnology);
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

  // Keyboard handler
  useEffect(() => {
    const handleKeyDown = (event: KeyboardEvent) => {
      if (event.key === 'Escape' && event.target instanceof HTMLTextAreaElement) {
        (event.target as HTMLTextAreaElement).blur();
        return;
      }
      if (event.target instanceof HTMLInputElement || event.target instanceof HTMLTextAreaElement) return;

      if (event.key === 'j' || event.key === 'ArrowDown') {
        event.preventDefault();
        setFocusedIdx(prev => {
          const next = Math.min(prev + 1, (gallery?.length || 1) - 1);
          cardRefs.current[next]?.scrollIntoView({ behavior: 'smooth', block: 'center' });
          return next;
        });
      } else if (event.key === 'k' || event.key === 'ArrowUp') {
        event.preventDefault();
        setFocusedIdx(prev => {
          const next = Math.max(prev - 1, 0);
          cardRefs.current[next]?.scrollIntoView({ behavior: 'smooth', block: 'center' });
          return next;
        });
      } else if (event.key === 'c' && focusedIdx >= 0 && gallery) {
        cardRefs.current[focusedIdx]?.querySelector('textarea')?.focus();
      } else if (event.key === '0' && focusedIdx >= 0 && gallery) {
        setReviewStatus(gallery[focusedIdx].id, focusedIdx, '');
      } else {
        const statusMatch = REVIEW_STATUSES.find(s => s.hotkey === event.key);
        if (statusMatch && focusedIdx >= 0 && gallery) {
          setReviewStatus(gallery[focusedIdx].id, focusedIdx, statusMatch.key);
        }
      }
    };

    document.addEventListener('keydown', handleKeyDown);
    return () => document.removeEventListener('keydown', handleKeyDown);
  }, [focusedIdx, gallery]);

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

    return (
      <div
        key={screenshot.id}
        ref={el => { cardRefs.current[idx] = el; }}
        className={cn(
          "transition-all",
          focusedIdx === idx && "ring-2 ring-blue-500 rounded-lg"
        )}
        onClick={() => setFocusedIdx(idx)}
      >
        <Card className={cn(
          "group overflow-hidden transition-all hover:shadow-lg flex flex-col h-full",
          getReviewBorderColor(screenshot.review_status)
        )}>
          {/* Review tag bar */}
          <div className="flex items-center gap-1 px-2 py-1 border-b" onClick={e => e.stopPropagation()}>
            {REVIEW_STATUSES.map(s => {
              const Icon = s.icon;
              const isActive = screenshot.review_status === s.key;
              return (
                <TooltipProvider key={s.key} delayDuration={0}>
                  <Tooltip>
                    <TooltipTrigger asChild>
                      <button
                        onClick={(e) => { e.preventDefault(); setReviewStatus(screenshot.id, idx, s.key); }}
                        className={cn(
                          "p-1 rounded transition-all border",
                          isActive ? s.bg : "border-transparent hover:border-muted-foreground/30"
                        )}
                      >
                        <Icon className={cn("w-3.5 h-3.5", isActive ? s.color : "text-muted-foreground")} />
                      </button>
                    </TooltipTrigger>
                    <TooltipContent side="bottom" className="text-xs">
                      <p>{s.label} ({s.hotkey})</p>
                    </TooltipContent>
                  </Tooltip>
                </TooltipProvider>
              );
            })}
            {screenshot.review_comment && (
              <MessageSquareIcon className="w-3 h-3 text-blue-400 ml-auto" />
            )}
          </div>

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
              <div className="w-full truncate text-xs text-muted-foreground mt-0.5">
                {screenshot.url}
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
            <div className="w-full mt-1" onClick={e => e.stopPropagation()}>
              <Textarea
                placeholder="comment..."
                value={screenshot.review_comment || ''}
                onChange={(e) => handleCommentChange(screenshot.id, idx, e.target.value)}
                className="text-xs min-h-[28px] max-h-[80px] resize-y font-mono"
                rows={1}
              />
            </div>
          </CardFooter>
        </Card>
      </div>
    );
  };



  if (loading) return <WideSkeleton />;

  return (
    <div className="space-y-6">
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
        <div className="text-sm text-muted-foreground">
          {gallery.length} / {totalCount}
        </div>
      </div>

      <div className="grid gap-6 md:grid-cols-2 lg:grid-cols-3 xl:grid-cols-4">
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
