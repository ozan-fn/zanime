import { z } from "zod";

import { createTRPCRouter, protectedProcedure, publicProcedure } from "~/server/api/trpc";
import { drives } from "../drive";
import { type Daum } from "~/types/mal";
import axios from "axios";
import { fromStream } from "file-type";
import { Readable } from "stream";

export const postRouter = createTRPCRouter({
    create: protectedProcedure.input(z.object({ driveId: z.string(), anime: z.custom<Daum>(), episode: z.number(), url: z.string() })).mutation(async ({ input, ctx }) => {
        const drive = await drives();
        const { driveId, anime, episode } = input;

        const folders = await drive.files.list({ driveId, supportsAllDrives: true, corpora: "drive", includeItemsFromAllDrives: true, q: `mimeType = 'application/vnd.google-apps.folder'` });
        let folder = folders.data.files?.find((f) => f.name == anime.mal_id + "");
        if (folder) folder = (await drive.files.create({ supportsAllDrives: true, requestBody: { parents: [driveId], name: anime.mal_id + "", mimeType: "application/vnd.google-apps.folder" } })).data;

        if (folder?.id) {
            await drive.permissions.update({ supportsAllDrives: true, fileId: folder.id });
            const files = await drive.files.list({ driveId, supportsAllDrives: true, corpora: "drive", includeItemsFromAllDrives: true, q: `'${folder.id}' in parents` });
            const file = files.data.files?.find((f) => f.name?.split(".")[0] == episode + "");
            if (file?.id) await drive.files.delete({ supportsAllDrives: true, fileId: file.id });

            const stream = await axios.get<Readable>(input.url, { responseType: "stream" });
            const fileType = await fromStream(stream.data);

            await drive.files.create({ supportsAllDrives: true, requestBody: { driveId, parents: [folder.id], name: `${episode}.${fileType?.ext}` }, media: { body: stream, mimeType: fileType?.mime } });
        }
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
