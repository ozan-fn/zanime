import { Router } from "express";
import { kuronimeCompleted, kuronimeEmbed, kuronimeEpisode, kuronimeOngoing, kuronimeSearch } from "../controllers/kuronimeController";

const kuronimeRoute = Router();

kuronimeRoute.get("/search", kuronimeSearch);
kuronimeRoute.get("/ongoing", kuronimeOngoing);
kuronimeRoute.get("/completed", kuronimeCompleted);
kuronimeRoute.get("/episode/:title", kuronimeEpisode);
kuronimeRoute.get("/episode/embed/:episode", kuronimeEmbed);

export default kuronimeRoute;
