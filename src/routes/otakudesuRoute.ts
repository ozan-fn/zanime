import { Router } from "express";
import { otakudesuCompleted, otakudesuEmbed, otakudesuEpisode, otakudesuOngoing, otakudesuSearch } from "../controllers/otakudesuController";

const otakudesuRouter = Router();

otakudesuRouter.get("/search", otakudesuSearch);
otakudesuRouter.get("/ongoing", otakudesuOngoing);
otakudesuRouter.get("/completed", otakudesuCompleted);
otakudesuRouter.get("/episode/:title", otakudesuEpisode);
otakudesuRouter.get("/episode/embed/:episode", otakudesuEmbed);

export default otakudesuRouter;
