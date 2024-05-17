import { type Prisma } from "@prisma/client";
import { z } from "zod";

import { createTRPCRouter, protectedProcedure, publicProcedure } from "~/server/api/trpc";
import { db } from "~/server/db";

export const animeRouter = createTRPCRouter({
    findMany: publicProcedure.query(async ({ ctx }) => {
        const animes = await ctx.db.anime.findMany();
        return animes;
    }),

    findUnique: protectedProcedure.input(z.custom<Prisma.AnimeFindUniqueArgs>()).mutation(async ({ input }) => {
        const anime = await db.anime.findUnique(input);
        return anime as Prisma.AnimeGetPayload<{ include: { Episode: true } }>;
    }),

    update: protectedProcedure.input(z.custom<Prisma.AnimeUpdateArgs>()).mutation(async ({ input }) => {
        const anime = await db.anime.update(input);
        return anime;
    }),

    upsert: protectedProcedure.input(z.custom<Prisma.AnimeUpsertArgs>()).mutation(async ({ input }) => {
        const anime = await db.anime.upsert(input);
        return anime;
    }),
});
