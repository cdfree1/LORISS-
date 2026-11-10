export const LIVE25_BUILDINGS_URL = new URL(
  "https://25live.collegenet.com/25live/data/unc/run/micro/building/list.json",
);

LIVE25_BUILDINGS_URL.searchParams.set("caller", "pro-BuildingService.getBuildings");
