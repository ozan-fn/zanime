import axios from "axios";
import { z } from "zod";

import { createTRPCRouter,  publicProcedure } from "~/server/api/trpc";
import { type AnimeById } from "~/types/AnimeById";
import { type Anime } from "~/types/mal";

export const malRouter = createTRPCRouter({
    getAnimeSearch: publicProcedure.input(z.string()).mutation(async ({ input }) => {
        const { data } = await axios<Anime>(`https://api.jikan.moe/v4/anime?q=${input}`);
        return data;
    }),

    getAnimeById: publicProcedure.input(z.string()).mutation(async ({ input }) => {
        const { data } = await axios<AnimeById>(`https://api.jikan.moe/v4/anime/${input}`);
        return data;
    }),
});
