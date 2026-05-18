import * as api from "@/lib/api/api";
import * as apitypes from "@/lib/api/types";
import { toast } from "@/hooks/use-toast";

const getWappalyzerData = async (
  setWappalyzer: React.Dispatch<React.SetStateAction<apitypes.wappalyzer | undefined>>,
  setTechnology: React.Dispatch<React.SetStateAction<apitypes.technologylist | undefined>>
) => {
  try {
    const [wappalyzerData, technologyData] = await Promise.all([
      await api.get('wappalyzer'),
      await api.get('technology')
    ]);
    setWappalyzer(wappalyzerData);
    setTechnology(technologyData);
  } catch (err) {
    toast({
      title: "API Error",
      variant: "destructive",
      description: `Failed to get wappalyzer / technology data: ${err}`
    });
  }
};

const getData = async (
  setLoading: React.Dispatch<React.SetStateAction<boolean>>,
  setGallery: React.Dispatch<React.SetStateAction<apitypes.galleryResult[] | undefined>>,
  setTotalPages: React.Dispatch<React.SetStateAction<number>>,
  page: number,
  limit: number,
  technologyFilter: string,
  statusFilter: string,
  perceptionGroup: boolean,
  showFailed: boolean,
  reviewFilter?: string,
) => {
  setLoading(true);
  try {
    const params: Record<string, string | number | boolean> = {
      page,
      limit,
      technologies: technologyFilter,
      status: statusFilter,
      perception: perceptionGroup ? 'true' : 'false',
      failed: showFailed ? 'true' : 'false',
    };
    if (reviewFilter) {
      params.review = reviewFilter;
    }
    const s = await api.get('gallery', params);
    setGallery(s.results);
    setTotalPages(Math.ceil(s.total_count / limit));
  } catch (err) {
    toast({
      title: "API Error",
      variant: "destructive",
      description: `Failed to get gallery: ${err}`
    });
  } finally {
    setLoading(false);
  }
};

export { getWappalyzerData, getData };
