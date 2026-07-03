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

export { getWappalyzerData };
