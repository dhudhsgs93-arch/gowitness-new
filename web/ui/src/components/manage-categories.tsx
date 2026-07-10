import { useEffect, useMemo, useState, useCallback } from "react";
import { TagsIcon, PlusIcon, SearchIcon, XIcon } from "lucide-react";
import { Dialog, DialogContent, DialogHeader, DialogTitle } from "@/components/ui/dialog";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";
import { cn } from "@/lib/utils";
import * as api from "@/lib/api/api";
import * as apitypes from "@/lib/api/types";
import { toast } from "@/hooks/use-toast";

interface ManageCategoriesProps {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  // Notify the parent (gallery) that categories changed, so it can refresh
  // its own category filter dropdown.
  onChanged?: () => void;
}

const DEFAULT_COLOR = "#ef4444";

// categoryPill renders a colored category badge. The tinted background is the
// category color at low opacity (hex + "22"), with the color as the text.
const categoryPill = (name: string, color: string) => (
  <span
    className="inline-flex items-center gap-1.5 rounded-full px-2.5 py-0.5 text-xs font-semibold"
    style={{ backgroundColor: `${color}22`, color }}
  >
    <span className="h-2 w-2 rounded-full" style={{ backgroundColor: color }} />
    {name}
  </span>
);

const ManageCategories = ({ open, onOpenChange, onChanged }: ManageCategoriesProps) => {
  const [categories, setCategories] = useState<apitypes.category[]>([]);
  const [domains, setDomains] = useState<apitypes.domainEntry[]>([]);
  const [selected, setSelected] = useState<Set<string>>(new Set());
  const [activeCatId, setActiveCatId] = useState<number | null>(null);
  const [filter, setFilter] = useState("");
  const [newName, setNewName] = useState("");
  const [newColor, setNewColor] = useState(DEFAULT_COLOR);

  const reload = useCallback(async () => {
    try {
      const [cats, doms] = await Promise.all([
        api.get("categories"),
        api.get("categoryDomains"),
      ]);
      setCategories(cats);
      setDomains(doms);
      onChanged?.();
    } catch (err) {
      toast({ title: "API Error", variant: "destructive", description: `Failed to load categories: ${err}` });
    }
  }, [onChanged]);

  useEffect(() => {
    if (open) reload();
  }, [open, reload]);

  const filteredDomains = useMemo(() => {
    const q = filter.trim().toLowerCase();
    if (!q) return domains;
    return domains.filter(d => d.domain.includes(q));
  }, [domains, filter]);

  const allVisibleSelected = filteredDomains.length > 0 && filteredDomains.every(d => selected.has(d.domain));

  const toggleDomain = (domain: string) => {
    setSelected(prev => {
      const next = new Set(prev);
      if (next.has(domain)) next.delete(domain); else next.add(domain);
      return next;
    });
  };

  const toggleAllVisible = () => {
    setSelected(prev => {
      const next = new Set(prev);
      if (allVisibleSelected) {
        filteredDomains.forEach(d => next.delete(d.domain));
      } else {
        filteredDomains.forEach(d => next.add(d.domain));
      }
      return next;
    });
  };

  const addCategory = async () => {
    const name = newName.trim();
    if (!name) return;
    try {
      await api.post("categoryCreate", { name, color: newColor });
      setNewName("");
      setNewColor(DEFAULT_COLOR);
      await reload();
      toast({ title: `Category "${name}" created`, duration: 1500 });
    } catch {
      toast({ title: "Error", description: "Could not create category (name may already exist)", variant: "destructive" });
    }
  };

  const deleteCategory = async (id: number) => {
    try {
      await api.post("categoryDelete", { id });
      if (activeCatId === id) setActiveCatId(null);
      await reload();
    } catch {
      toast({ title: "Error", description: "Could not delete category", variant: "destructive" });
    }
  };

  const assignSelected = async () => {
    if (!activeCatId) {
      toast({ title: "Pick a category first", description: "Select a category on the left or in the dropdown.", duration: 2000 });
      return;
    }
    if (selected.size === 0) return;
    try {
      const res = await api.post("categoryAssign", { domains: Array.from(selected), category_id: activeCatId });
      setSelected(new Set());
      await reload();
      toast({ title: `Assigned ${res.count} domain(s)`, duration: 1500 });
    } catch {
      toast({ title: "Error", description: "Assign failed", variant: "destructive" });
    }
  };

  const unassignDomains = async (targets: string[]) => {
    if (targets.length === 0) return;
    try {
      await api.post("categoryUnassign", { domains: targets });
      setSelected(prev => {
        const next = new Set(prev);
        targets.forEach(t => next.delete(t));
        return next;
      });
      await reload();
    } catch {
      toast({ title: "Error", description: "Unassign failed", variant: "destructive" });
    }
  };

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="max-w-6xl w-[95vw] h-[88vh] p-0 flex flex-col gap-0 overflow-hidden">
        <DialogHeader className="flex flex-row items-center gap-3 border-b px-6 py-4 space-y-0">
          <div className="flex h-10 w-10 items-center justify-center rounded-xl bg-primary/10 text-primary">
            <TagsIcon className="h-5 w-5" />
          </div>
          <DialogTitle className="text-2xl font-bold">Manage host categories</DialogTitle>
        </DialogHeader>

        <div className="grid flex-1 grid-cols-1 gap-0 overflow-hidden md:grid-cols-[340px_1fr]">
          {/* Categories column */}
          <div className="flex flex-col overflow-hidden border-r">
            <div className="px-6 pt-5 pb-2 text-xs font-semibold uppercase tracking-wider text-muted-foreground">
              Categories
            </div>
            <div className="flex-1 space-y-2 overflow-y-auto px-4 pb-2">
              {categories.length === 0 && (
                <p className="px-2 py-4 text-sm text-muted-foreground">No categories yet. Create one below.</p>
              )}
              {categories.map(c => (
                <div
                  key={c.id}
                  onClick={() => setActiveCatId(c.id)}
                  className={cn(
                    "group flex cursor-pointer items-center gap-3 rounded-lg border p-3 transition-colors hover:bg-muted/60",
                    activeCatId === c.id && "ring-2 ring-primary bg-muted/40"
                  )}
                >
                  <span className="h-8 w-8 shrink-0 rounded-md" style={{ backgroundColor: c.color }} />
                  <span className="flex-1 truncate font-semibold">{c.name}</span>
                  <span className="rounded-md bg-muted px-2 py-0.5 text-xs text-muted-foreground">{c.domain_count} dom</span>
                  <span className="rounded-md bg-muted px-2 py-0.5 text-xs text-muted-foreground">{c.host_count} hosts</span>
                  <button
                    onClick={(e) => { e.stopPropagation(); deleteCategory(c.id); }}
                    className="text-muted-foreground opacity-0 transition-opacity hover:text-destructive group-hover:opacity-100"
                    title="Delete category"
                  >
                    <XIcon className="h-4 w-4" />
                  </button>
                </div>
              ))}
            </div>
            {/* New category */}
            <div className="flex items-center gap-2 border-t p-4">
              <input
                type="color"
                value={newColor}
                onChange={(e) => setNewColor(e.target.value)}
                className="h-9 w-9 shrink-0 cursor-pointer rounded-md border bg-transparent p-0.5"
                title="Category color"
              />
              <Input
                placeholder="New category name"
                value={newName}
                onChange={(e) => setNewName(e.target.value)}
                onKeyDown={(e) => { if (e.key === "Enter") addCategory(); }}
              />
              <Button onClick={addCategory} disabled={!newName.trim()}>
                <PlusIcon className="mr-1 h-4 w-4" /> Add
              </Button>
            </div>
          </div>

          {/* Domains column */}
          <div className="flex flex-col overflow-hidden">
            <div className="px-6 pt-5 pb-2 text-xs font-semibold uppercase tracking-wider text-muted-foreground">
              Domains
            </div>
            <div className="flex flex-wrap items-center gap-2 px-6 pb-3">
              <div className="relative flex-1 min-w-[200px]">
                <SearchIcon className="absolute left-2.5 top-2.5 h-4 w-4 text-muted-foreground" />
                <Input
                  placeholder="Filter domains…"
                  value={filter}
                  onChange={(e) => setFilter(e.target.value)}
                  className="pl-8"
                />
              </div>
              <Select
                value={activeCatId ? String(activeCatId) : ""}
                onValueChange={(v) => setActiveCatId(Number(v))}
              >
                <SelectTrigger className="w-[180px]">
                  <SelectValue placeholder="Select category" />
                </SelectTrigger>
                <SelectContent>
                  {categories.map(c => (
                    <SelectItem key={c.id} value={String(c.id)}>
                      <span className="flex items-center gap-2">
                        <span className="h-2.5 w-2.5 rounded-full" style={{ backgroundColor: c.color }} />
                        {c.name}
                      </span>
                    </SelectItem>
                  ))}
                </SelectContent>
              </Select>
              <Button onClick={assignSelected} disabled={selected.size === 0 || !activeCatId}>
                Assign selected
              </Button>
              <Button variant="outline" onClick={() => unassignDomains(Array.from(selected))} disabled={selected.size === 0}>
                Unassign
              </Button>
            </div>

            <div className="flex-1 overflow-y-auto border-t">
              <table className="w-full text-sm">
                <thead className="sticky top-0 z-10 bg-background">
                  <tr className="border-b text-xs uppercase tracking-wider text-muted-foreground">
                    <th className="w-10 px-4 py-2 text-left">
                      <input
                        type="checkbox"
                        checked={allVisibleSelected}
                        onChange={toggleAllVisible}
                        className="h-4 w-4 cursor-pointer accent-primary"
                      />
                    </th>
                    <th className="px-2 py-2 text-left font-semibold">Domain</th>
                    <th className="px-2 py-2 text-right font-semibold">Hosts</th>
                    <th className="px-4 py-2 text-left font-semibold">Category</th>
                  </tr>
                </thead>
                <tbody>
                  {filteredDomains.map(d => (
                    <tr key={d.domain} className="border-b hover:bg-muted/40">
                      <td className="px-4 py-2">
                        <input
                          type="checkbox"
                          checked={selected.has(d.domain)}
                          onChange={() => toggleDomain(d.domain)}
                          className="h-4 w-4 cursor-pointer accent-primary"
                        />
                      </td>
                      <td className="px-2 py-2 font-mono">{d.domain}</td>
                      <td className="px-2 py-2 text-right tabular-nums text-muted-foreground">{d.hosts}</td>
                      <td className="px-4 py-2">
                        {d.category_id ? (
                          <span className="inline-flex items-center gap-1">
                            {categoryPill(d.category_name, d.category_color)}
                            <button
                              onClick={() => unassignDomains([d.domain])}
                              className="text-muted-foreground hover:text-destructive"
                              title="Unassign"
                            >
                              <XIcon className="h-3.5 w-3.5" />
                            </button>
                          </span>
                        ) : (
                          <span className="rounded-full bg-muted px-2.5 py-0.5 text-xs text-muted-foreground">Uncategorized</span>
                        )}
                      </td>
                    </tr>
                  ))}
                  {filteredDomains.length === 0 && (
                    <tr>
                      <td colSpan={4} className="px-4 py-8 text-center text-muted-foreground">No domains found.</td>
                    </tr>
                  )}
                </tbody>
              </table>
            </div>
          </div>
        </div>
      </DialogContent>
    </Dialog>
  );
};

export default ManageCategories;
