import { useEffect, useMemo, useRef, useState } from "react";
import { Link, useLoaderData, useNavigation } from "react-router-dom";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Textarea } from "@/components/ui/textarea";
import { Tooltip, TooltipContent, TooltipProvider, TooltipTrigger } from "@/components/ui/tooltip";
import {
  CheckCircle2Icon, AlertTriangleIcon, StarIcon, SkullIcon, Trash2Icon, MessageSquareIcon,
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

export default function SearchResultsPage() {
  const data = useLoaderData() as apitypes.searchresult[] | { error: string } | undefined;
  const navigation = useNavigation();

  const [results, setResults] = useState<apitypes.searchresult[]>([]);
  const [reviewFilter, setReviewFilter] = useState<string>("");
  const saveTimers = useRef<Record<number, ReturnType<typeof setTimeout>>>({});

  // sync local state whenever a new search loads
  useEffect(() => {
    if (Array.isArray(data)) {
      setResults(data);
      setReviewFilter("");
    } else {
      setResults([]);
    }
  }, [data]);

  // counts for the filter pills, computed from the loaded result set
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

  if (navigation.state === 'loading') return <WideSkeleton />;

  if (!Array.isArray(data) || data.length === 0) {
    return <div className="text-center mt-8">No results found.</div>;
  }

  return (
    <div className="mx-auto p-4">
      <h1 className="text-2xl font-bold mb-3">
        Search Results <span className="text-muted-foreground">({filtered.length}{reviewFilter ? ` / ${results.length}` : ""})</span>
      </h1>

      {/* Review status filter pills */}
      <div className="sticky top-16 z-40 bg-background/95 backdrop-blur border-b mb-4 -mx-4 px-4 py-2">
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
              <Button
                key={s.key}
                variant={reviewFilter === s.key ? "secondary" : "outline"}
                size="sm"
                onClick={() => setReviewFilter(s.key)}
                className="h-7 text-xs"
              >
                <Icon className={cn("w-3 h-3 mr-1", s.color)} />
                {counts[s.key] || 0}
              </Button>
            );
          })}
          <Button variant={reviewFilter === 'commented' ? "secondary" : "outline"} size="sm" onClick={() => setReviewFilter('commented')} className="h-7 text-xs">
            <MessageSquareIcon className="w-3 h-3 mr-1 text-blue-400" />
            {counts.commented}
          </Button>
        </div>
      </div>

      <div className="grid gap-4 md:grid-cols-2 lg:grid-cols-4">
        {filtered.map((result) => (
          <Card key={result.id} className={cn("flex flex-col h-full transition-shadow hover:shadow-lg group", getReviewBorderColor(result.review_status))}>
            {/* Review tag bar */}
            <div className="flex items-center gap-1 px-2 py-1 border-b">
              {REVIEW_STATUSES.map(s => {
                const Icon = s.icon;
                const isActive = result.review_status === s.key;
                return (
                  <TooltipProvider key={s.key} delayDuration={0}>
                    <Tooltip>
                      <TooltipTrigger asChild>
                        <button
                          onClick={(e) => { e.preventDefault(); setReviewStatus(result.id, s.key); }}
                          className={cn(
                            "p-1 rounded transition-all border",
                            isActive ? s.bg : "border-transparent hover:border-muted-foreground/30"
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
              {result.review_comment && <MessageSquareIcon className="w-3 h-3 text-blue-400 ml-auto" />}
            </div>

            <Link to={`/screenshot/${result.id}`}>
              <CardHeader className="relative p-0">
                <img
                  src={
                    result.screenshot
                      ? `data:image/png;base64,${result.screenshot}`
                      : api.endpoints.screenshot.path + "/" + result.file_name}
                  alt={result.url}
                  loading="lazy"
                  className="w-full h-48 object-cover transition-all duration-300 filter group-hover:scale-105"
                />
                <Badge className={`absolute top-2 right-2 ${getStatusColor(result.response_code)} text-white`}>
                  {result.response_code}
                </Badge>
              </CardHeader>
            </Link>

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
              <Textarea
                placeholder="comment..."
                value={result.review_comment || ''}
                onChange={(e) => handleCommentChange(result.id, e.target.value)}
                className="text-xs min-h-[28px] max-h-[80px] resize-y font-mono mt-1"
                rows={1}
              />
            </CardContent>
          </Card>
        ))}
      </div>
    </div>
  );
}
