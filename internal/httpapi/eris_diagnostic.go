package httpapi

import (
	"context"
	"fmt"

	"github.com/hloiseau/le-concierge/internal/eris"
	"github.com/hloiseau/le-concierge/internal/store"
	"github.com/hloiseau/le-concierge/internal/upstream"
)

func (s *Server) erisDiagnostic(ctx context.Context, u store.User, grant upstream.Grant, p eris.Plan) eris.ToolReply {
	failure := eris.ToolReply{Content: "Je n’ai pas pu vérifier où en est cette demande. Une source manque ou ne répond pas ; je ne peux pas confirmer la cause du blocage."}
	member, err := s.Upstream.SeerrUser(ctx, s.Config.SeerrURL, s.Config.SeerrAPIKey, u.PlexID)
	if err != nil {
		return failure
	}
	requestID := p.RequestID
	title := fmt.Sprintf("Demande #%d", requestID)
	if requestID == 0 {
		items, err := s.Upstream.FindPersonalRequests(ctx, s.Config.SeerrURL, s.Config.SeerrAPIKey, member, p.Query)
		if err != nil {
			return failure
		}
		if len(items) != 1 {
			text := "Je n’identifie pas une seule demande sur ton compte. Précise le titre ou son numéro :"
			if len(items) == 0 {
				text = "Je ne trouve pas de demande correspondante sur ton compte. Vérifie son titre ou son numéro dans /mes-demandes."
			}
			for i, item := range items {
				if i == 3 {
					break
				}
				text += fmt.Sprintf("\n• #%d · %s · %s", item.ID, safeText(item.Title), statusLabel(item.Status))
			}
			return eris.ToolReply{Content: text}
		}
		requestID, title = items[0].ID, safeText(items[0].Title)
		if items[0].Status == "pending" {
			return eris.ToolReply{Content: "**" + title + "** : la demande attend une approbation. Le téléchargement ne commence pas avant cette validation ; un administrateur peut l’examiner."}
		}
		if items[0].Status == "declined" {
			return eris.ToolReply{Content: "**" + title + "** : la demande a été refusée. Aucun téléchargement ne découle de cette demande ; un administrateur peut t’expliquer la décision."}
		}
	}
	result, err := s.Upstream.DiagnosePersonalRequest(ctx, s.diagnosticSources(), grant, requestID, member)
	if err != nil {
		return failure
	}
	text := "**" + title + "**\n" + diagnosticTeaching(result.Stage)
	if result.ProgressPercent != nil {
		text += fmt.Sprintf("\nProgression observée : %.0f %% des données des téléchargements consultés. Cela ne signifie pas encore disponible dans Plex.", *result.ProgressPercent)
	}
	if result.MissingEpisodes != nil && *result.MissingEpisodes > 0 {
		text += fmt.Sprintf("\n%d épisode(s) sans fichier dans les saisons demandées, d’après les compteurs du service.", *result.MissingEpisodes)
	}
	if result.PlexStatus == "indexed" {
		text += "\nLa fiche est indexée dans Plex et accessible à ton compte ; la lecture et la complétude des saisons ne sont pas vérifiées ici."
	}
	if result.QueueTruncated || result.HistoryTruncated {
		text += "\nLecture partielle : la file ou l’historique dépasse la limite consultée."
	}
	return eris.ToolReply{Content: text}
}

func diagnosticTeaching(stage string) string {
	switch stage {
	case "approval":
		return "La demande attend une approbation. Un administrateur doit la valider avant le téléchargement."
	case "declined":
		return "La demande est refusée. Un administrateur peut expliquer la décision."
	case "downloading":
		return "Les données sont en cours de téléchargement. L’import puis l’indexation dans Plex viennent ensuite."
	case "queued":
		return "Le téléchargement attend son tour ou un délai prévu. Il n’a pas encore commencé à recevoir les données."
	case "paused":
		return "Le téléchargement est en pause. Il ne progresse pas tant qu’il reste dans cet état ; un administrateur peut vérifier pourquoi."
	case "sources":
		return "Le client signale un manque de sources : il a trouvé le fichier, mais pas assez de personnes qui en partagent les données. Il peut rester en attente."
	case "manual_import":
		return "Le fichier a été trouvé, mais l’application ne peut pas l’associer et l’importer automatiquement. Un administrateur doit vérifier le bon contenu avant un import manuel."
	case "permissions":
		return "L’import n’arrive pas à accéder à son fichier ou à son dossier. C’est une vérification de droits ou de montage à faire par un administrateur."
	case "space":
		return "L’import manque d’espace disponible. Un administrateur doit vérifier le stockage avant de poursuivre."
	case "rename_import":
		return "Le déplacement ou le renommage du fichier a échoué pendant l’import. Un administrateur doit vérifier cette étape."
	case "importing":
		return "L’import traite le fichier ou signale un blocage. Cette étape range et reconnaît le média avant que Plex puisse l’indexer ; je ne peux pas confirmer une cause plus précise."
	case "download_failed":
		return "Le téléchargement signale un échec. Un administrateur peut examiner la cause et décider de la suite ; je ne relance rien automatiquement."
	case "client_unavailable":
		return "Le service ne parvient pas à joindre le client de téléchargement pour ce média. Je ne peux pas confirmer sa progression actuelle."
	case "plex_indexing":
		return "Un fichier est répertorié, mais sa disponibilité complète dans Plex n’est pas confirmée. Le rangement et l’indexation peuvent encore être en cours."
	case "imported":
		return "Un import est enregistré pour cette demande. Cela ne prouve pas encore que tous ses éléments sont lisibles dans Plex."
	case "indexers":
		return "Le service signale une difficulté avec les sources qui l’aident à trouver les fichiers. Cela peut retarder la recherche, sans prouver à lui seul la cause de cette demande."
	case "not_found":
		return "Le média n’est pas trouvé dans le service de traitement configuré. Un administrateur peut vérifier où la demande a été envoyée, notamment pour la 4K."
	default:
		return "Je n’ai pas de cause confirmée dans les éléments consultés. Une recherche peut être encore en attente ; l’absence d’erreur ne prouve pas que tout fonctionne."
	}
}
