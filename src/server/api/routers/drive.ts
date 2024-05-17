import { z } from "zod";

import { createTRPCRouter, protectedProcedure, publicProcedure } from "~/server/api/trpc";
import { drives } from "../drive";

export const driveRouter = createTRPCRouter({
    teamdrives: protectedProcedure.query(async () => {
        const drive = await drives();
        return await drive.teamdrives.list();
    }),

    // getLatest: protectedProcedure.query(({ ctx }) => {
    //   return ctx.db.post.findFirst({
    //     orderBy: { createdAt: "desc" },
    //     where: { createdBy: { id: ctx.session.user.id } },
    //   });
    // }),

    // getSecretMessage: protectedProcedure.query(() => {
    //   return "you can now see this secret message!";
    // }),
});
